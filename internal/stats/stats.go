// Package stats computes language statistics for a repository's file list.
// All functions are pure: they take explicit inputs and return structured results.
package stats

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// languageMap maps file extensions to language display names.
var languageMap = map[string]string{
	".py": "Python", ".js": "JavaScript", ".jsx": "JavaScript",
	".ts": "TypeScript", ".tsx": "TypeScript",
	".rs": "Rust", ".go": "Go", ".java": "Java",
	".kt": "Kotlin", ".swift": "Swift",
	".c": "C", ".h": "C", ".cpp": "C++", ".hpp": "C++",
	".cs": "C#", ".rb": "Ruby", ".php": "PHP",
	".sh": "Shell", ".bash": "Shell", ".zsh": "Shell", ".fish": "Shell",
	".md": "Markdown", ".toml": "TOML",
	".yaml": "YAML", ".yml": "YAML",
	".json": "JSON", ".html": "HTML",
	".css": "CSS", ".scss": "CSS", ".sass": "CSS",
	".xml": "XML", ".sql": "SQL",
	".r": "R", ".jl": "Julia",
	".ex": "Elixir", ".exs": "Elixir",
	".hs": "Haskell", ".lua": "Lua",
	".dart": "Dart", ".zig": "Zig",
	".tf": "Terraform", ".vue": "Vue", ".svelte": "Svelte",
}

// filenameLanguageMap maps specific filenames (without extension) to language names.
var filenameLanguageMap = map[string]string{
	"Makefile": "Make", "makefile": "Make",
	"Dockerfile": "Docker", "dockerfile": "Docker",
	"Justfile": "Just", "justfile": "Just",
}

// LangStats holds per-language file and line counts.
type LangStats struct {
	Files int
	Lines int
}

// Stats aggregates repository-wide file and language statistics.
type Stats struct {
	TotalFiles int
	TotalLines int
	Languages  map[string]LangStats
}

// detectLanguage returns the display name for the language of path, or "" if unknown.
func detectLanguage(path string) string {
	base := filepath.Base(path)
	if lang, ok := filenameLanguageMap[base]; ok {
		return lang
	}
	return languageMap[strings.ToLower(filepath.Ext(base))]
}

// Compute counts files and lines for the given absolute paths rooted at root.
// It detects the language of each file by extension or filename.
func Compute(root string, files []string) Stats {
	s := Stats{
		TotalFiles: len(files),
		Languages:  make(map[string]LangStats),
	}
	for _, abs := range files {
		content, err := os.ReadFile(abs)
		lines := 0
		if err == nil && len(content) > 0 {
			lines = strings.Count(string(content), "\n") + 1
		}
		s.TotalLines += lines

		lang := detectLanguage(abs)
		if lang != "" {
			ls := s.Languages[lang]
			ls.Files++
			ls.Lines += lines
			s.Languages[lang] = ls
		}
	}
	return s
}

// FormatMarkdown renders a Stats value as a Markdown summary with a language table.
func FormatMarkdown(s Stats) string {
	if len(s.Languages) == 0 {
		return fmt.Sprintf("- **Files:** %d\n- **Lines:** %d\n", s.TotalFiles, s.TotalLines)
	}

	// Sort languages by line count descending.
	type entry struct {
		name string
		LangStats
	}
	entries := make([]entry, 0, len(s.Languages))
	for name, ls := range s.Languages {
		entries = append(entries, entry{name, ls})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Lines > entries[j].Lines
	})

	var sb strings.Builder
	fmt.Fprintf(&sb, "- **Total files:** %d\n", s.TotalFiles)
	fmt.Fprintf(&sb, "- **Total lines:** %d\n\n", s.TotalLines)
	sb.WriteString("| Language | Files | Lines | % |\n")
	sb.WriteString("|----------|------:|------:|--:|\n")
	for _, e := range entries {
		pct := 0.0
		if s.TotalLines > 0 {
			pct = float64(e.Lines) / float64(s.TotalLines) * 100
		}
		fmt.Fprintf(&sb, "| %s | %d | %d | %.1f%% |\n", e.name, e.Files, e.Lines, pct)
	}
	return sb.String()
}
