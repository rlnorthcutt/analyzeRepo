package stats

import (
	"strings"
	"testing"
)

// ── detectLanguage ────────────────────────────────────────────────────────────

func TestDetectLanguage_byExtension(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"main.go", "Go"},
		{"app.py", "Python"},
		{"index.ts", "TypeScript"},
		{"index.js", "JavaScript"},
		{"App.tsx", "TypeScript"},
		{"App.jsx", "JavaScript"},
		{"style.css", "CSS"},
		{"layout.html", "HTML"},
		{"notes.md", "Markdown"},
		{"config.yaml", "YAML"},
		{"config.yml", "YAML"},
		{"config.toml", "TOML"},
		{"config.json", "JSON"},
		{"query.sql", "SQL"},
		{"script.sh", "Shell"},
		{"build.rs", "Rust"},
		{"Main.java", "Java"},
		{"Program.cs", "C#"},
		{"main.cpp", "C++"},
		{"main.c", "C"},
		{"unknownfile.xyz", ""},
	}
	for _, c := range cases {
		got := detectLanguage(c.path)
		if got != c.want {
			t.Errorf("detectLanguage(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

func TestDetectLanguage_byFilename(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"Makefile", "Make"},
		{"makefile", "Make"},
		{"Dockerfile", "Docker"},
		{"dockerfile", "Docker"},
		// go.mod / go.sum have no entry in the maps; language is unknown.
		{"go.mod", ""},
		{"go.sum", ""},
	}
	for _, c := range cases {
		got := detectLanguage(c.path)
		if got != c.want {
			t.Errorf("detectLanguage(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

// ── FormatMarkdown ────────────────────────────────────────────────────────────

func TestFormatMarkdown_noLanguages(t *testing.T) {
	s := Stats{TotalFiles: 3, TotalLines: 100, Languages: map[string]LangStats{}}
	out := FormatMarkdown(s)

	if !strings.Contains(out, "3") {
		t.Error("output should contain file count")
	}
	if !strings.Contains(out, "100") {
		t.Error("output should contain line count")
	}
	// No table when there are no detected languages.
	if strings.Contains(out, "|") {
		t.Error("expected no table when no languages detected")
	}
}

func TestFormatMarkdown_withLanguages(t *testing.T) {
	s := Stats{
		TotalFiles: 5,
		TotalLines: 200,
		Languages: map[string]LangStats{
			"Go":         {Files: 3, Lines: 150},
			"Markdown":   {Files: 2, Lines: 50},
		},
	}
	out := FormatMarkdown(s)

	if !strings.Contains(out, "Go") {
		t.Error("output should contain 'Go'")
	}
	if !strings.Contains(out, "Markdown") {
		t.Error("output should contain 'Markdown'")
	}
	// Table headers present.
	if !strings.Contains(out, "Language") {
		t.Error("output should contain table header 'Language'")
	}
	// Go should appear before Markdown (more lines).
	goIdx := strings.Index(out, "Go")
	mdIdx := strings.Index(out, "Markdown")
	if goIdx > mdIdx {
		t.Error("Go (150 lines) should appear before Markdown (50 lines) in table")
	}
}

func TestFormatMarkdown_percentages(t *testing.T) {
	s := Stats{
		TotalFiles: 2,
		TotalLines: 100,
		Languages: map[string]LangStats{
			"Go":     {Files: 1, Lines: 75},
			"Python": {Files: 1, Lines: 25},
		},
	}
	out := FormatMarkdown(s)

	if !strings.Contains(out, "75") {
		t.Error("expected Go line count 75 in output")
	}
	if !strings.Contains(out, "25") {
		t.Error("expected Python line count 25 in output")
	}
}
