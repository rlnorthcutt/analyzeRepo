// Package report writes the three Markdown output files produced by repo-analyze.
// All functions are pure writers: they take structured data and write files,
// returning the output path or an error.
package report

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rlnorthcutt/analyzeRepo/internal/analyze"
	"github.com/rlnorthcutt/analyzeRepo/internal/stats"
)

// mdPurpose maps normalized Markdown file stems to human-readable descriptions
// used in the Documentation Library section of ONBOARDING.md.
var mdPurpose = map[string]string{
	"README": "Project overview and getting started guide",
	"CHANGELOG": "Changelog / release history", "CHANGES": "Changelog / release history",
	"HISTORY": "Release history", "CONTRIBUTING": "Contribution guidelines",
	"CONTRIBUTORS": "List of project contributors", "LICENSE": "License information",
	"CLAUDE": "AI coding assistant instructions", "ARCHITECTURE": "Architecture and design overview",
	"DESIGN": "Design documentation", "API": "API reference",
	"SECURITY": "Security policy", "CODE_OF_CONDUCT": "Code of conduct",
	"SUPPORT": "Support guide", "INSTALL": "Installation guide",
	"INSTALLATION": "Installation guide", "DEPLOYMENT": "Deployment guide",
	"DEPLOY": "Deployment guide", "TESTING": "Testing guide",
	"ROADMAP": "Project roadmap", "FAQ": "Frequently asked questions",
	"GLOSSARY": "Glossary of terms", "TROUBLESHOOTING": "Troubleshooting guide",
	"MIGRATION": "Migration guide", "UPGRADE": "Upgrade guide",
	"RELEASE": "Release notes", "SETUP": "Setup guide",
	"QUICKSTART": "Quick start guide", "GETTING_STARTED": "Getting started guide",
	"GUIDE": "User guide", "REFERENCE": "Reference documentation",
	"DEVELOPMENT": "Development guide", "DEV": "Development guide",
	"WORKFLOW": "Workflow documentation",
}

// roleIcons maps file roles to short badge labels used in ANALYSIS.md headings.
var roleIcons = map[string]string{
	"entrypoint": "[entry]", "core": "[core]", "config": "[config]",
	"test": "[test]", "docs": "[docs]", "util": "[util]",
	"data": "[data]", "build": "[build]", "other": "[other]",
}

// suggestionIcons maps suggestion types to short prefix labels.
var suggestionIcons = map[string]string{
	"improvement": "[+]", "refactor": "[~]", "security": "[!]",
	"performance": "[>]", "docs": "[d]",
}

// roleOrder defines the grouping order in ANALYSIS.md.
var roleOrder = []string{
	"entrypoint", "core", "config", "build", "util", "data", "test", "docs", "other",
}

// guessMDPurpose returns a human-readable description for a Markdown file path
// based on its filename stem.
func guessMDPurpose(relPath string) string {
	stem := filepath.Base(relPath)
	stem = strings.TrimSuffix(stem, filepath.Ext(stem))
	stem = strings.ToUpper(strings.NewReplacer("-", "_", " ", "_").Replace(stem))

	if desc, ok := mdPurpose[stem]; ok {
		return desc
	}
	for key, desc := range mdPurpose {
		if strings.Contains(stem, key) {
			return desc
		}
	}
	parts := strings.Split(filepath.ToSlash(relPath), "/")
	if len(parts) > 1 {
		dir := strings.ToLower(parts[0])
		if dir == "docs" || dir == "documentation" || dir == "doc" {
			return "Documentation"
		}
	}
	return "Documentation"
}

// buildMDLibrary renders a Documentation Library section listing all .md files.
func buildMDLibrary(mdFiles []string) string {
	if len(mdFiles) == 0 {
		return ""
	}
	sorted := make([]string, len(mdFiles))
	copy(sorted, mdFiles)
	sort.Strings(sorted)

	var sb strings.Builder
	sb.WriteString("## Documentation Library\n\n")
	for _, path := range sorted {
		fmt.Fprintf(&sb, "- `%s` — %s\n", path, guessMDPurpose(path))
	}
	sb.WriteString("\n")
	return sb.String()
}

// capitalize returns s with its first letter uppercased.
func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// WriteAnalysis writes ANALYSIS.md with per-file summaries grouped by role.
// Returns the output path on success.
func WriteAnalysis(analyses []analyze.FileAnalysis, outDir string) (string, error) {
	byRole := make(map[string][]analyze.FileAnalysis)
	for _, a := range analyses {
		role := a.Role
		if role == "" {
			role = "other"
		}
		byRole[role] = append(byRole[role], a)
	}

	var sb strings.Builder
	sb.WriteString("# File Summaries\n\n")

	for _, role := range roleOrder {
		entries, ok := byRole[role]
		if !ok {
			continue
		}
		icon := roleIcons[role]
		if icon == "" {
			icon = "[other]"
		}
		fmt.Fprintf(&sb, "## %s %s\n\n", icon, capitalize(role))

		for _, a := range entries {
			fmt.Fprintf(&sb, "### `%s`\n\n", a.Path)
			sb.WriteString(a.Summary)
			sb.WriteString("\n\n")

			if len(a.Suggestions) > 0 {
				sb.WriteString("**Suggestions:**\n\n")
				for _, s := range a.Suggestions {
					icon := suggestionIcons[s.Type]
					if icon == "" {
						icon = "[-]"
					}
					// Header line: icon, type, file, effort, and optional blocks.
					header := fmt.Sprintf("- %s **%s**", icon, s.Type)
					if s.File != "" {
						header += " | file: `" + s.File + "`"
					}
					if s.Effort != "" {
						header += " | effort: " + s.Effort
					}
					if s.Blocks != "" {
						header += " | blocks: " + s.Blocks
					}
					sb.WriteString(header + "\n")
					if s.Description != "" {
						sb.WriteString("  " + s.Description + "\n")
					}
					if s.DoneWhen != "" {
						sb.WriteString("  `done_when:` " + s.DoneWhen + "\n")
					}
					sb.WriteString("\n")
				}
			}
			sb.WriteString("---\n\n")
		}
	}

	outPath := filepath.Join(outDir, "ANALYSIS.md")
	if err := os.WriteFile(outPath, []byte(sb.String()), 0644); err != nil {
		return "", fmt.Errorf("writing ANALYSIS.md: %w", err)
	}
	return outPath, nil
}

// WriteOnboarding writes ONBOARDING.md with the executive summary, repository
// statistics, documentation library, and file tree. Returns the output path.
func WriteOnboarding(
	summary string,
	st stats.Stats,
	treeStr, repoName, outDir string,
	mdFiles []string,
) (string, error) {
	statsSection := stats.FormatMarkdown(st)
	mdLibrary := buildMDLibrary(mdFiles)

	content := fmt.Sprintf(
		"# %s — Onboarding Guide\n\n%s\n\n## Repository Statistics\n\n%s\n%s## File Tree\n\n```\n%s\n```\n",
		repoName, summary, statsSection, mdLibrary, treeStr,
	)

	outPath := filepath.Join(outDir, "ONBOARDING.md")
	if err := os.WriteFile(outPath, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("writing ONBOARDING.md: %w", err)
	}
	return outPath, nil
}

// WriteClaudeMD writes the generated CLAUDE.md content to outDir.
// Returns the output path.
func WriteClaudeMD(content, outDir string) (string, error) {
	outPath := filepath.Join(outDir, "CLAUDE.md")
	if err := os.WriteFile(outPath, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("writing CLAUDE.md: %w", err)
	}
	return outPath, nil
}
