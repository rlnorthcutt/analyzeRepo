// Package cmd defines the repo-analyze CLI command using Cobra.
// It resolves configuration (flags → interactive wizard → defaults)
// and orchestrates the five-step analysis pipeline.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/rlnorthcutt/analyzeRepo/internal/analyze"
	"github.com/rlnorthcutt/analyzeRepo/internal/fetch"
	"github.com/rlnorthcutt/analyzeRepo/internal/report"
	"github.com/rlnorthcutt/analyzeRepo/internal/stats"
	"github.com/rlnorthcutt/analyzeRepo/internal/tui"
)

// ANSI escape codes for styled terminal output.
const (
	ansiReset  = "\033[0m"
	ansiBold   = "\033[1m"
	ansiDim    = "\033[2m"
	ansiRed    = "\033[31m"
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
	ansiCyan   = "\033[36m"
)

// flags bound to the root command.
var (
	flagReports        string
	flagFull           bool
	flagOutput         string
	flagNonInteractive bool
	flagDryRun         bool
)

// Execute runs the root command. Called by main.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "%sError:%s %v\n", ansiRed, ansiReset, err)
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:   "analyzerepo [source]",
	Short: "Onboard any codebase with Claude AI",
	Long: ansiBold + "analyzerepo" + ansiReset + `

Generates developer onboarding documentation for any GitHub repository
or local codebase using Claude AI.

Output files (written to the output directory):
  ONBOARDING.md — executive summary, language stats, and file tree
  ANALYSIS.md   — per-file role summaries and improvement suggestions
  CLAUDE.md     — AI assistant context file for use with Claude Code`,
	Args:          cobra.MaximumNArgs(1),
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE:          run,
}

func init() {
	rootCmd.Flags().StringVar(&flagReports, "reports", "",
		"reports to generate: onboarding, improvement, claude, all (default: all)")
	rootCmd.Flags().BoolVar(&flagFull, "full", false,
		"analyze all files (default: Claude selects key files)")
	rootCmd.Flags().StringVarP(&flagOutput, "output", "o", "",
		"output directory for report files (default: current directory)")
	rootCmd.Flags().BoolVar(&flagNonInteractive, "non-interactive", false,
		"skip all prompts; use defaults for any unset flags")
	rootCmd.Flags().BoolVar(&flagDryRun, "dry-run", false,
		"walk through the full pipeline without calling Claude (for testing)")
}

// run resolves configuration from flags and interactive prompts, then calls pipeline.
func run(cmd *cobra.Command, args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Detect interactive mode: stdin must be a TTY, --non-interactive must be
	// unset, and --dry-run must be unset (dry-run is a testing shortcut that
	// skips all prompts).
	stat, _ := os.Stdin.Stat()
	isTTY := (stat.Mode() & os.ModeCharDevice) != 0
	interactive := isTTY && !flagNonInteractive && !flagDryRun

	tui.PrintBanner()

	// ── Determine which flags were explicitly set ────────────────────────────
	reportsSet := cmd.Flags().Changed("reports")
	fullSet := cmd.Flags().Changed("full")
	outputSet := cmd.Flags().Changed("output")

	// ── Build initial Config from any already-set flags ──────────────────────
	pre := tui.Config{
		Full:      flagFull,
		OutputDir: flagOutput,
	}
	if len(args) > 0 {
		pre.Source = strings.TrimSpace(args[0])
	}
	if reportsSet {
		pre.Reports = parseReports(flagReports)
		if pre.Reports == nil {
			return fmt.Errorf("unknown report type in %q — valid: onboarding, improvement, claude, all", flagReports)
		}
	}

	// ── Run wizard for any unresolved values (interactive only) ──────────────
	var cfg tui.Config
	if interactive {
		// Walk the user through every step that wasn't already provided via a
		// flag or positional arg. skipSource is true when the source was given
		// as a positional argument; the other skip flags mirror the Changed()
		// state of their respective flags.
		skipSource := pre.Source != ""
		var wizErr error
		cfg, wizErr = tui.RunWizard(pre, skipSource, reportsSet, fullSet, outputSet)
		if wizErr != nil {
			return wizErr
		}
		if cfg.Source == "" {
			return errors.New("source cannot be empty")
		}
	} else {
		// Non-interactive path: apply defaults for anything unset.
		cfg = pre
		if cfg.Source == "" {
			// --dry-run skips the full wizard but we can still ask for the one
			// required value if we have a TTY. --non-interactive never prompts.
			if isTTY && !flagNonInteractive {
				cfg.Source = promptString("GitHub URL or local path", "")
			}
			if cfg.Source == "" {
				return errors.New("missing required argument SOURCE\nUsage: analyzerepo <github-url-or-path>")
			}
		}
		if cfg.Reports == nil {
			cfg.Reports = map[string]bool{"onboarding": true, "improvement": true, "claude": true}
		}
		if cfg.OutputDir == "" {
			cfg.OutputDir = "."
		}
	}

	// ── Ensure output directory exists ───────────────────────────────────────
	if err := os.MkdirAll(cfg.OutputDir, 0755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}
	absOut, _ := filepath.Abs(cfg.OutputDir)

	return pipeline(ctx, cfg, absOut)
}

// parseReports converts a comma-separated report flag value to a selection map.
// Returns nil if any token is invalid.
func parseReports(raw string) map[string]bool {
	valid := map[string]bool{"onboarding": true, "improvement": true, "claude": true, "all": true}
	selected := make(map[string]bool)
	for _, r := range strings.Split(raw, ",") {
		r = strings.TrimSpace(strings.ToLower(r))
		if !valid[r] {
			return nil
		}
		if r == "all" {
			return map[string]bool{"onboarding": true, "improvement": true, "claude": true}
		}
		selected[r] = true
	}
	return selected
}

// pipeline executes the full analysis workflow.
func pipeline(ctx context.Context, cfg tui.Config, outDir string) error {
	// Create Claude client (detects API key or CLI, or stub for dry-run).
	var client *analyze.Client
	if flagDryRun {
		printWarn("dry-run mode — Claude will not be called")
		client = analyze.NewDryRunClient()
	} else {
		var err error
		client, err = analyze.NewClient()
		if err != nil {
			return err
		}
	}

	// ── Phase 1: Discovery ────────────────────────────────────────────────────

	printPhase(1, 3, "Discovery")

	printActive("Acquiring %s", cfg.Source)
	repoPath, cleanup, err := fetch.GetRepo(ctx, cfg.Source)
	if err != nil {
		return fmt.Errorf("acquiring repo: %w", err)
	}
	defer cleanup()

	files, err := fetch.BuildFileList(repoPath)
	if err != nil {
		return fmt.Errorf("building file list: %w", err)
	}
	if len(files) == 0 {
		return errors.New("no analyzable files found in repository")
	}

	treeStr := fetch.BuildTreeString(repoPath, files)
	repoStats := stats.Compute(repoPath, files)
	repoName := filepath.Base(repoPath)

	printDone("%s  ·  %s files  ·  %s lines",
		filepath.Base(repoPath),
		formatTokenCount(int64(repoStats.TotalFiles)),
		formatTokenCount(int64(repoStats.TotalLines)),
	)

	var selected []string
	if cfg.Full {
		selected = files
		printDone("All %d files queued for analysis", len(selected))
	} else {
		printActive("Selecting key files")
		selected, err = analyze.SelectKeyFiles(ctx, client, repoPath, files, treeStr)
		if err != nil || len(selected) == 0 {
			printWarn("file selection failed (%v), using first 20 files", err)
			n := min(20, len(files))
			selected = files[:n]
		}
		printDone("%d files selected for analysis", len(selected))
	}

	if !flagDryRun {
		estCalls, estTokens := analyze.EstimateUsage(selected, cfg.Reports)
		printDim("  ~ %d calls  ·  ~%s input tokens estimated\n", estCalls, formatTokenCount(estTokens))
	}

	// ── Phase 2: Analysis ─────────────────────────────────────────────────────

	printPhase(2, 3, "Analysis")

	analyses, failed, err := tui.RunPipeline(ctx, client, repoPath, selected, actionVerbs())
	if err != nil {
		return fmt.Errorf("pipeline: %w", err)
	}
	if failed > 0 {
		printWarn("%d file(s) could not be analyzed", failed)
	}

	// If the user interrupted (Ctrl+C) during analysis, stop here.
	if ctx.Err() != nil {
		return fmt.Errorf("interrupted")
	}

	// Collect .md files for the Documentation Library section.
	var mdFiles []string
	for _, abs := range files {
		rel, _ := filepath.Rel(repoPath, abs)
		if strings.ToLower(filepath.Ext(rel)) == ".md" {
			mdFiles = append(mdFiles, rel)
		}
	}

	// ── Phase 3: Generation ───────────────────────────────────────────────────

	printPhase(3, 3, "Generation")

	type writtenFile struct {
		name, path, desc string
	}
	var written []writtenFile

	if cfg.Reports["improvement"] {
		outPath := filepath.Join(outDir, "ANALYSIS.md")
		if confirmOverwrite(outPath) {
			path, err := report.WriteAnalysis(analyses, outDir)
			if err != nil {
				printWarn("ANALYSIS.md: %v", err)
			} else {
				printDone("ANALYSIS.md")
				written = append(written, writtenFile{"ANALYSIS.md", path, "Possible fixes & architecture notes"})
			}
		}
	}

	if cfg.Reports["onboarding"] {
		outPath := filepath.Join(outDir, "ONBOARDING.md")
		if confirmOverwrite(outPath) {
			printActive("Generating onboarding guide")
			summary, err := analyze.GenerateExecutiveSummary(ctx, client, analyses, repoStats, treeStr)
			if err != nil {
				printWarn("executive summary failed (%v)", err)
				summary = "_Executive summary generation failed._"
			}
			path, err := report.WriteOnboarding(summary, repoStats, treeStr, repoName, outDir, mdFiles)
			if err != nil {
				printWarn("ONBOARDING.md: %v", err)
			} else {
				printDone("ONBOARDING.md")
				written = append(written, writtenFile{"ONBOARDING.md", path, "Getting started & file map"})
			}
		}
	}

	if cfg.Reports["claude"] {
		outPath := filepath.Join(outDir, "CLAUDE.md")
		if confirmOverwrite(outPath) {
			printActive("Generating CLAUDE.md")
			content, err := analyze.GenerateClaudeMD(ctx, client, analyses, repoStats, treeStr)
			if err != nil {
				printWarn("CLAUDE.md generation failed (%v)", err)
				content = "# CLAUDE.md\n\n_Generation failed._"
			}
			path, err := report.WriteClaudeMD(content, outDir)
			if err != nil {
				printWarn("CLAUDE.md: %v", err)
			} else {
				printDone("CLAUDE.md")
				written = append(written, writtenFile{"CLAUDE.md", path, "Optimized context for Claude Code"})
			}
		}
	}

	// ── Summary ───────────────────────────────────────────────────────────────

	// Compute a display-friendly output directory label.
	cwd, _ := os.Getwd()
	displayDir := outDir
	if rel, err := filepath.Rel(cwd, outDir); err == nil {
		if !strings.HasPrefix(rel, "..") {
			displayDir = "./" + rel
		}
	}

	fmt.Printf("\n  %s%s%s\n\n", ansiDim, strings.Repeat("─", 57), ansiReset)
	fmt.Printf("  %s%s✔ Success!%s %d report(s) created in %s%s%s\n\n",
		ansiBold, ansiGreen, ansiReset,
		len(written),
		ansiCyan, displayDir, ansiReset,
	)

	// Pad filenames to align the descriptions.
	maxName := 0
	for _, f := range written {
		if len(f.name) > maxName {
			maxName = len(f.name)
		}
	}
	for _, f := range written {
		padding := strings.Repeat(" ", maxName-len(f.name))
		link := termLink("file://"+f.path, "📁 "+f.name)
		fmt.Printf("  %s%s  %s→%s  %s%s%s\n",
			link, padding,
			ansiDim, ansiReset,
			ansiDim, f.desc, ansiReset,
		)
	}

	if !flagDryRun {
		usage := client.GetUsage()
		fmt.Printf("\n  %sClaude%s  %d calls  ·  %s in  ·  %s out\n",
			ansiDim, ansiReset,
			usage.Calls,
			formatTokenCount(usage.InputTokens),
			formatTokenCount(usage.OutputTokens),
		)
	}

	fmt.Println()
	return nil
}

// ── Output helpers ────────────────────────────────────────────────────────────

// printPhase prints a bold phase header followed by a dim separator line.
func printPhase(n, total int, name string) {
	fmt.Printf("\n  %s%sPhase %d/%d%s  %s\n",
		ansiBold, ansiCyan, n, total, ansiReset, name)
	fmt.Printf("  %s%s%s\n\n", ansiDim, strings.Repeat("─", 50), ansiReset)
}

// printActive prints a "▸ doing X" line for an in-progress step.
func printActive(format string, args ...any) {
	fmt.Printf("  "+ansiCyan+"▸ "+ansiReset+format+"\n", args...)
}

// printDone prints an indented "✓ X" line for a completed step.
func printDone(format string, args ...any) {
	fmt.Printf("    "+ansiGreen+"✓ "+ansiReset+format+"\n", args...)
}

func printDim(format string, args ...any) {
	fmt.Printf(ansiDim+format+ansiReset, args...)
}

func printWarn(format string, args ...any) {
	fmt.Printf(ansiYellow+"  Warning: "+ansiReset+format+"\n", args...)
}

// confirmOverwrite returns true if it is safe to write to path.
// If the file already exists and stdin is an interactive TTY, the user is asked.
// In non-interactive or non-TTY contexts the file is silently overwritten.
func confirmOverwrite(path string) bool {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return true // file doesn't exist yet
	}
	stat, _ := os.Stdin.Stat()
	isTTY := (stat.Mode() & os.ModeCharDevice) != 0
	if !isTTY || flagNonInteractive {
		return true // non-interactive: overwrite without asking
	}
	fmt.Printf("  %s%s already exists. Overwrite?%s [y/N]: ",
		ansiYellow, filepath.Base(path), ansiReset)
	var resp string
	fmt.Scanln(&resp)
	return strings.EqualFold(strings.TrimSpace(resp), "y")
}

// promptString prints a prompt and reads a trimmed line from stdin.
// Returns defaultVal if the user enters an empty line.
func promptString(prompt, defaultVal string) string {
	if defaultVal != "" {
		fmt.Printf("%s [%s]: ", prompt, defaultVal)
	} else {
		fmt.Printf("%s: ", prompt)
	}
	var line string
	fmt.Scanln(&line)
	line = strings.TrimSpace(line)
	if line == "" {
		return defaultVal
	}
	return line
}

// termLink wraps text in an OSC 8 terminal hyperlink pointing to url.
// Supported by iTerm2, VS Code integrated terminal, Kitty, and most modern terminals.
func termLink(url, text string) string {
	return "\033]8;;" + url + "\033\\" + text + "\033]8;;\033\\"
}

// formatTokenCount formats a token count with a K or M suffix for readability.
func formatTokenCount(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

// min returns the smaller of a and b.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// actionVerbs is a list of flavour verbs shown next to each file during analysis.
func actionVerbs() []string {
	return []string{
		"Analyzing", "Auditing", "Combing thru", "Decoding", "Digesting",
		"Dissecting", "Evaluating", "Examining", "Exploring", "Inspecting",
		"Interpreting", "Investigating", "Mapping", "Parsing", "Probing",
		"Processing", "Reviewing", "Scanning", "Scrutinizing", "Studying",
		"Summarizing", "Synthesizing", "Traversing", "Unpacking",
	}
}
