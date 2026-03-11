// Package analyze handles all Claude AI interactions: backend detection,
// per-file analysis, key file selection, and report generation.
// All exported functions are pure in their logic — side effects are limited
// to network calls and reading the files they are explicitly given.
package analyze

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"

	anthropic "github.com/anthropics/anthropic-sdk-go"

	"github.com/rlnorthcutt/analyzeRepo/internal/stats"
)

const (
	// model is the Claude model used for all analysis calls.
	model = "claude-opus-4-6"

	// maxFileBytes is the maximum number of characters read from a single file.
	// Content beyond this limit is truncated before sending to Claude.
	maxFileBytes = 8_000
)

// Suggestion represents an actionable improvement recommendation for a file.
type Suggestion struct {
	Type        string `json:"type"`
	File        string `json:"file"`   // target file (may differ from the analyzed file)
	Effort      string `json:"effort"` // trivial | small | medium | large
	Description string `json:"description"`
	DoneWhen    string `json:"done_when"`        // single verifiable completion condition
	Blocks      string `json:"blocks,omitempty"` // downstream concern blocked by this change
}

// FileAnalysis holds the structured analysis result for a single source file.
type FileAnalysis struct {
	Path        string       `json:"path"`
	Role        string       `json:"role"`
	Summary     string       `json:"summary"`
	Suggestions []Suggestion `json:"suggestions"`
}

// backendKind identifies which Claude backend a Client will use.
type backendKind int

const (
	backendAPI backendKind = iota
	backendCLI
)

// Usage holds accumulated token and call counts for a Client session.
type Usage struct {
	InputTokens  int64
	OutputTokens int64
	Calls        int64
}

// Client wraps the available Claude backends.
// The API key backend takes priority over the CLI backend.
type Client struct {
	api          anthropic.Client // valid when hasAPI is true
	hasAPI       bool             // true when ANTHROPIC_API_KEY is set
	hasCLI       bool
	primary      backendKind
	DryRun       bool // when true, all calls return stub data without hitting Claude
	inputTokens  atomic.Int64
	outputTokens atomic.Int64
	calls        atomic.Int64
}

// GetUsage returns a snapshot of accumulated token usage and call counts.
func (c *Client) GetUsage() Usage {
	return Usage{
		InputTokens:  c.inputTokens.Load(),
		OutputTokens: c.outputTokens.Load(),
		Calls:        c.calls.Load(),
	}
}

// NewClient detects the available Claude backend and returns a ready Client.
// The API key (ANTHROPIC_API_KEY env var) takes priority; the Claude CLI
// is used as a fallback. Returns an error if neither is available.
func NewClient() (*Client, error) {
	c := &Client{}

	if os.Getenv("ANTHROPIC_API_KEY") != "" {
		c.api = anthropic.NewClient() // reads ANTHROPIC_API_KEY automatically
		c.hasAPI = true
		c.primary = backendAPI
	}

	if _, err := exec.LookPath("claude"); err == nil {
		c.hasCLI = true
		if !c.hasAPI {
			c.primary = backendCLI
		}
	}

	if !c.hasAPI && !c.hasCLI {
		return nil, errors.New(
			"no Claude backend available — set ANTHROPIC_API_KEY or " +
				"install the Claude CLI (https://claude.ai/code)",
		)
	}
	return c, nil
}

// NewDryRunClient returns a Client that never contacts Claude.
// All analysis functions return plausible stub data so the full pipeline
// can be exercised without an API key or network access.
func NewDryRunClient() *Client {
	return &Client{DryRun: true}
}

// call sends prompt to Claude and returns the text response.
// It tries the API first (if configured), falling back to the CLI on auth failure.
func (c *Client) call(ctx context.Context, prompt string, maxTokens int64) (string, error) {
	if c.primary == backendAPI {
		text, err := c.callAPI(ctx, prompt, maxTokens)
		if err != nil {
			if isAuthError(err) && c.hasCLI {
				// API key present but invalid — fall back to CLI.
				return c.callCLI(ctx, prompt)
			}
			return "", err
		}
		return text, nil
	}
	return c.callCLI(ctx, prompt)
}

// callAPI sends the prompt through the Anthropic SDK.
func (c *Client) callAPI(ctx context.Context, prompt string, maxTokens int64) (string, error) {
	resp, err := c.api.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeOpus4_6,
		MaxTokens: maxTokens,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	})
	if err != nil {
		return "", fmt.Errorf("claude API: %w", err)
	}

	c.inputTokens.Add(resp.Usage.InputTokens)
	c.outputTokens.Add(resp.Usage.OutputTokens)
	c.calls.Add(1)

	var sb strings.Builder
	for _, block := range resp.Content {
		switch v := block.AsAny().(type) {
		case anthropic.TextBlock:
			sb.WriteString(v.Text)
		}
	}
	return sb.String(), nil
}

// cliJSONResponse is the top-level structure returned by `claude --output-format json`.
type cliJSONResponse struct {
	Result string `json:"result"`
	Usage  struct {
		InputTokens  int64 `json:"input_tokens"`
		OutputTokens int64 `json:"output_tokens"`
	} `json:"usage"`
}

// callCLI sends the prompt to the `claude` CLI subprocess.
// It strips the CLAUDECODE env var so nested invocations work inside a Claude Code session.
func (c *Client) callCLI(ctx context.Context, prompt string) (string, error) {
	// --tools "" disables all built-in tools so the CLI acts as a pure text
	// generator rather than spinning up an agentic session that may call Write.
	// The prompt is fed via stdin because --tools is variadic and would
	// otherwise consume the positional prompt argument as a tool name.
	// --output-format json lets us capture token usage from the response.
	cmd := exec.CommandContext(ctx, "claude", "-p", "--no-session-persistence",
		"--output-format", "json", "--tools", "")
	cmd.Stdin = strings.NewReader(prompt)
	cmd.Env = filterEnv("CLAUDECODE")
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
			return "", fmt.Errorf("claude CLI: %s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", fmt.Errorf("claude CLI: %w", err)
	}

	var parsed cliJSONResponse
	if jsonErr := json.Unmarshal(out, &parsed); jsonErr != nil {
		// JSON parsing failed — fall back to treating the output as plain text.
		return strings.TrimSpace(string(out)), nil
	}

	c.inputTokens.Add(parsed.Usage.InputTokens)
	c.outputTokens.Add(parsed.Usage.OutputTokens)
	c.calls.Add(1)

	return parsed.Result, nil
}

// isAuthError reports whether err indicates an HTTP 401 authentication failure.
func isAuthError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "authentication_error") ||
		strings.Contains(msg, "status 401") ||
		strings.Contains(msg, "401")
}

// filterEnv returns os.Environ() with any variable named key removed.
func filterEnv(key string) []string {
	env := os.Environ()
	out := make([]string, 0, len(env))
	prefix := key + "="
	for _, e := range env {
		if !strings.HasPrefix(e, prefix) {
			out = append(out, e)
		}
	}
	return out
}

// stripFences removes leading/trailing markdown code fences from text.
func stripFences(text string) string {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "```") {
		if i := strings.Index(text, "\n"); i != -1 {
			text = text[i+1:]
		}
		text = strings.TrimSuffix(strings.TrimSpace(text), "```")
	}
	return strings.TrimSpace(text)
}

// SelectKeyFiles asks Claude to identify the most important files (up to 20)
// from the given list. files must be absolute paths; root is the repo root.
// Falls back to returning files unchanged if selection fails.
func SelectKeyFiles(ctx context.Context, c *Client, root string, files []string, treeStr string) ([]string, error) {
	if c.DryRun {
		n := min(20, len(files))
		return files[:n], nil
	}

	relPaths := make([]string, 0, len(files))
	for _, abs := range files {
		rel, _ := filepath.Rel(root, abs)
		relPaths = append(relPaths, rel)
	}

	prompt := fmt.Sprintf(`You are analyzing a codebase. Given the file tree and file list below, select the most
important files for understanding the architecture and purpose of the project.

Return a JSON object with a single key "files" containing an array of relative file paths.
Choose at most 20 files. Prioritize: entry points, core logic, configuration, and documentation.
Exclude: generated files, assets, and boilerplate.

File tree:
%s

All files:
%s

Respond with only a raw JSON object, no markdown fences. Example:
{"files": ["src/main.go", "README.md", "go.mod"]}`,
		treeStr, strings.Join(relPaths, "\n"))

	text, err := c.call(ctx, prompt, 1024)
	if err != nil {
		return nil, err
	}

	var result struct {
		Files []string `json:"files"`
	}
	if err := json.Unmarshal([]byte(stripFences(text)), &result); err != nil {
		return nil, fmt.Errorf("parsing key file selection: %w", err)
	}

	// Map selected relative paths back to absolute paths.
	selectedSet := make(map[string]bool, len(result.Files))
	for _, r := range result.Files {
		selectedSet[r] = true
	}
	var selected []string
	for _, abs := range files {
		rel, _ := filepath.Rel(root, abs)
		if selectedSet[rel] {
			selected = append(selected, abs)
		}
	}
	return selected, nil
}

// AnalyzeFile sends a single file to Claude and returns structured analysis.
// root is the repo root; relPath is the file path relative to root.
func AnalyzeFile(ctx context.Context, c *Client, root, relPath string) (FileAnalysis, error) {
	if c.DryRun {
		return FileAnalysis{
			Path:    relPath,
			Role:    dryRunRole(relPath),
			Summary: fmt.Sprintf("[dry-run] %s — analysis skipped.", relPath),
			Suggestions: []Suggestion{
				{Type: "improvement", Description: "Run without --dry-run for real suggestions."},
			},
		}, nil
	}

	content, err := os.ReadFile(filepath.Join(root, relPath))
	if err != nil {
		return FileAnalysis{}, fmt.Errorf("reading %s: %w", relPath, err)
	}

	text := string(content)
	if len(text) > maxFileBytes {
		text = text[:maxFileBytes] + "\n... [truncated]"
	}

	prompt := fmt.Sprintf(`Analyze the following source file and return a JSON object with this exact schema:
{
  "path": "<relative file path>",
  "role": "<one of: entrypoint, core, config, test, docs, util, data, build, other>",
  "summary": "<2-3 sentence description of what this file does>",
  "suggestions": [
    {
      "type": "<one of: improvement, refactor, security, performance, docs>",
      "file": "<target file path — usually the same as the analyzed file, but use a different path if the fix belongs there>",
      "effort": "<one of: trivial, small, medium, large>",
      "description": "<concise actionable suggestion>",
      "done_when": "<single verifiable condition: a grep, a visual check, a validation pass — something checkable>",
      "blocks": "<optional: a downstream concern this change must precede — omit the field entirely if none>"
    }
  ]
}

File: %s

`+"```"+`
%s
`+"```"+`

Respond with only a raw JSON object, no markdown fences.`, relPath, text)

	resp, err := c.call(ctx, prompt, 1024)
	if err != nil {
		return FileAnalysis{}, err
	}

	var analysis FileAnalysis
	if err := json.Unmarshal([]byte(stripFences(resp)), &analysis); err != nil {
		return FileAnalysis{}, fmt.Errorf("parsing analysis for %s: %w", relPath, err)
	}
	analysis.Path = relPath // ensure path is canonical
	return analysis, nil
}

// GenerateExecutiveSummary produces a Markdown prose summary of the repository
// suitable for inclusion in ONBOARDING.md.
func GenerateExecutiveSummary(ctx context.Context, c *Client, analyses []FileAnalysis, s stats.Stats, treeStr string) (string, error) {
	if c.DryRun {
		return "## Purpose\n\n_[dry-run] Executive summary skipped — no Claude call made._\n\n" +
			"## Architecture\n\n_Run without `--dry-run` to generate a real summary._\n", nil
	}
	prompt := fmt.Sprintf(`Write a developer onboarding summary for the codebase described below.

IMPORTANT: Output ONLY raw Markdown prose. Do not use tools. Do not write to any file.
Do not include a top-level H1 heading (the report wrapper adds one).

Cover these sections:
1. **Purpose** — What does this project do?
2. **Architecture** — How is it structured? What are the key components?
3. **Key Patterns** — Notable design decisions, conventions, or patterns.
4. **Getting Started** — Where should a new developer start reading?

Languages: %s
Total files analyzed: %d

File tree:
%s

File analyses:
%s`,
		langSummary(s), s.TotalFiles, treeStr, analysesSummary(analyses))

	text, err := c.call(ctx, prompt, 2048)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(text), nil
}

// GenerateClaudeMD produces a CLAUDE.md context file for AI coding assistants.
func GenerateClaudeMD(ctx context.Context, c *Client, analyses []FileAnalysis, s stats.Stats, treeStr string) (string, error) {
	if c.DryRun {
		return "# CLAUDE.md\n\n_[dry-run] CLAUDE.md generation skipped — no Claude call made._\n\n" +
			"Run without `--dry-run` to generate real content.\n", nil
	}
	prompt := fmt.Sprintf(`Generate the complete raw Markdown content of a CLAUDE.md file for the codebase described below.

IMPORTANT: Output ONLY the raw file content. Do not use tools. Do not write to any file.
Do not describe what you will write. Your entire response must be the Markdown content
itself, starting with the line "# CLAUDE.md".

A good CLAUDE.md covers:
1. Project overview (1-2 sentences)
2. Key architecture decisions and patterns
3. Directory structure with role of each folder/file
4. Development conventions (naming, patterns, tooling)
5. Common tasks (how to run, test, build)
6. What to avoid (gotchas, anti-patterns)

File tree:
%s

File analyses:
%s

Top suggestions from analysis:
%s`,
		treeStr, analysesSummary(analyses), suggestionsSummary(analyses))

	text, err := c.call(ctx, prompt, 4096)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(text), nil
}

// analysesSummary formats file analyses as a compact bullet list.
func analysesSummary(analyses []FileAnalysis) string {
	var sb strings.Builder
	for _, a := range analyses {
		fmt.Fprintf(&sb, "- %s (%s): %s\n", a.Path, a.Role, a.Summary)
	}
	return sb.String()
}

// langSummary formats language statistics as a comma-separated string.
func langSummary(s stats.Stats) string {
	type entry struct {
		name  string
		lines int
	}
	entries := make([]entry, 0, len(s.Languages))
	for name, ls := range s.Languages {
		entries = append(entries, entry{name, ls.Lines})
	}
	// Simple descending sort by line count.
	for i := 0; i < len(entries); i++ {
		for j := i + 1; j < len(entries); j++ {
			if entries[j].lines > entries[i].lines {
				entries[i], entries[j] = entries[j], entries[i]
			}
		}
	}
	parts := make([]string, 0, len(entries))
	for _, e := range entries {
		if ls, ok := s.Languages[e.name]; ok {
			parts = append(parts, fmt.Sprintf("%s (%d files)", e.name, ls.Files))
		}
	}
	return strings.Join(parts, ", ")
}

// suggestionsSummary formats the top 20 suggestions across all analyses.
func suggestionsSummary(analyses []FileAnalysis) string {
	var sb strings.Builder
	count := 0
	for _, a := range analyses {
		for _, s := range a.Suggestions {
			if count >= 20 {
				break
			}
			fmt.Fprintf(&sb, "- [%s] %s\n", s.Type, s.Description)
			count++
		}
		if count >= 20 {
			break
		}
	}
	if sb.Len() == 0 {
		return "None"
	}
	return sb.String()
}

// ── Usage estimation ──────────────────────────────────────────────────────────

// EstimateUsage returns a rough pre-run estimate of Claude API calls and input
// tokens for the given file set and report selection.
//
// The estimate uses os.Stat (no file reads) to get sizes, caps each file at
// maxFileBytes, and adds a constant prompt-template overhead per call.
// Report-generation calls are included with fixed overhead estimates.
func EstimateUsage(files []string, reports map[string]bool) (calls int, inputTokens int64) {
	const (
		promptOverheadChars = 2_000 // template text, path, JSON schema in each prompt
		charsPerToken       = 4     // rough approximation
	)

	for _, abs := range files {
		size := int64(maxFileBytes) // pessimistic default
		if info, err := os.Stat(abs); err == nil {
			size = info.Size()
		}
		if size > maxFileBytes {
			size = maxFileBytes
		}
		inputTokens += int64(promptOverheadChars+size) / charsPerToken
		calls++
	}

	// Report generation calls (fixed overhead estimates).
	if reports["onboarding"] {
		calls++
		inputTokens += 3_000 // analyses summary + tree
	}
	if reports["claude"] {
		calls++
		inputTokens += 4_000 // analyses + tree + suggestions
	}

	return calls, inputTokens
}

// ── Dry-run helpers ───────────────────────────────────────────────────────────

// dryRunRole guesses a file role from its name and extension so dry-run output
// looks plausible without touching Claude.
func dryRunRole(relPath string) string {
	name := strings.ToLower(filepath.Base(relPath))
	ext := strings.ToLower(filepath.Ext(relPath))

	switch {
	case name == "main.go" || name == "main.py" || name == "index.js" || name == "index.ts":
		return "entrypoint"
	case strings.Contains(name, "test") || strings.Contains(name, "_spec"):
		return "test"
	case ext == ".md" || ext == ".txt" || ext == ".rst" || ext == ".adoc":
		return "docs"
	case name == "makefile" || name == "dockerfile" ||
		ext == ".yaml" || ext == ".yml" || ext == ".sh":
		return "build"
	case strings.Contains(name, "config") || ext == ".toml" ||
		ext == ".env" || ext == ".ini" || ext == ".json":
		return "config"
	case strings.Contains(name, "util") || strings.Contains(name, "helper"):
		return "util"
	default:
		return "core"
	}
}
