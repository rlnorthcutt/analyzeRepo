package analyze

import (
	"strings"
	"testing"

	"github.com/rlnorthcutt/analyzeRepo/internal/stats"
)

// ── stripFences ───────────────────────────────────────────────────────────────

func TestStripFences(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "no fences",
			in:   "plain text",
			want: "plain text",
		},
		{
			name: "json fence",
			in:   "```json\n{\"key\": \"value\"}\n```",
			want: "{\"key\": \"value\"}",
		},
		{
			name: "bare fence",
			in:   "```\nsome content\n```",
			want: "some content",
		},
		{
			name: "whitespace around fenced block",
			in:   "  \n```go\npackage main\n```\n  ",
			want: "package main",
		},
		{
			name: "no closing fence leaves content",
			in:   "```\njust content",
			want: "just content",
		},
		{
			name: "empty string",
			in:   "",
			want: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := stripFences(c.in)
			if got != c.want {
				t.Errorf("stripFences(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// ── dryRunRole ────────────────────────────────────────────────────────────────

func TestDryRunRole(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"main.go", "entrypoint"},
		{"cmd/main.py", "entrypoint"},
		{"src/index.js", "entrypoint"},
		{"src/index.ts", "entrypoint"},
		{"foo_test.go", "test"},
		{"internal/fetch/fetch_test.go", "test"},
		{"spec/app_spec.rb", "test"},
		{"README.md", "docs"},
		{"docs/guide.txt", "docs"},
		{"Makefile", "build"},
		{"Dockerfile", "build"},
		{"deploy.yml", "build"},
		{"build.sh", "build"},
		{"config.toml", "config"},
		{"app.config.json", "config"},
		// .yaml extension is treated as "build" (CI/deploy files).
		{"config/settings.yaml", "build"},
		// .env.example has extension .example, no config match → "core".
		{".env.example", "core"},
		{"internal/utils/helpers.go", "util"},
		{"pkg/helper.go", "util"},
		{"internal/server/server.go", "core"},
		{"pkg/auth/auth.go", "core"},
	}
	for _, c := range cases {
		got := dryRunRole(c.path)
		if got != c.want {
			t.Errorf("dryRunRole(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

// ── analysesSummary ───────────────────────────────────────────────────────────

func TestAnalysesSummary_empty(t *testing.T) {
	got := analysesSummary(nil)
	if got != "" {
		t.Errorf("expected empty string for nil analyses, got %q", got)
	}
}

func TestAnalysesSummary_format(t *testing.T) {
	analyses := []FileAnalysis{
		{Path: "main.go", Role: "entrypoint", Summary: "Entry point."},
		{Path: "utils.go", Role: "util", Summary: "Utilities."},
	}
	got := analysesSummary(analyses)

	if !strings.Contains(got, "main.go") {
		t.Error("missing main.go")
	}
	if !strings.Contains(got, "entrypoint") {
		t.Error("missing role 'entrypoint'")
	}
	if !strings.Contains(got, "Entry point.") {
		t.Error("missing summary text")
	}
	// Each entry on its own line.
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 lines, got %d", len(lines))
	}
}

// ── langSummary ───────────────────────────────────────────────────────────────

func TestLangSummary_empty(t *testing.T) {
	got := langSummary(stats.Stats{Languages: map[string]stats.LangStats{}})
	if got != "" {
		t.Errorf("expected empty string for no languages, got %q", got)
	}
}

func TestLangSummary_sortedByLines(t *testing.T) {
	s := stats.Stats{
		Languages: map[string]stats.LangStats{
			"Python": {Files: 1, Lines: 10},
			"Go":     {Files: 3, Lines: 300},
			"YAML":   {Files: 2, Lines: 50},
		},
	}
	got := langSummary(s)

	goIdx := strings.Index(got, "Go")
	yamlIdx := strings.Index(got, "YAML")
	pyIdx := strings.Index(got, "Python")

	if !(goIdx < yamlIdx && yamlIdx < pyIdx) {
		t.Errorf("expected Go > YAML > Python by lines; positions: Go=%d YAML=%d Python=%d", goIdx, yamlIdx, pyIdx)
	}
}

func TestLangSummary_includesFileCount(t *testing.T) {
	s := stats.Stats{
		Languages: map[string]stats.LangStats{
			"Go": {Files: 7, Lines: 500},
		},
	}
	got := langSummary(s)
	if !strings.Contains(got, "7 files") {
		t.Errorf("expected file count in output, got %q", got)
	}
}

// ── EstimateUsage ─────────────────────────────────────────────────────────────

func TestEstimateUsage_noFiles(t *testing.T) {
	calls, tokens := EstimateUsage(nil, map[string]bool{})
	if calls != 0 {
		t.Errorf("expected 0 calls for no files, got %d", calls)
	}
	if tokens != 0 {
		t.Errorf("expected 0 tokens for no files, got %d", tokens)
	}
}

func TestEstimateUsage_fileCallsAndReports(t *testing.T) {
	// Use non-existent paths: EstimateUsage falls back to maxFileBytes for missing files.
	files := []string{"/nonexistent/a.go", "/nonexistent/b.go"}

	// No reports.
	calls, tokens := EstimateUsage(files, map[string]bool{})
	if calls != 2 {
		t.Errorf("expected 2 calls (one per file), got %d", calls)
	}
	if tokens <= 0 {
		t.Errorf("expected positive token estimate, got %d", tokens)
	}

	// With onboarding + claude reports: 2 extra calls.
	calls2, tokens2 := EstimateUsage(files, map[string]bool{"onboarding": true, "claude": true})
	if calls2 != calls+2 {
		t.Errorf("expected %d calls with reports, got %d", calls+2, calls2)
	}
	if tokens2 <= tokens {
		t.Errorf("expected more tokens with reports")
	}
}

func TestEstimateUsage_tokensPositive(t *testing.T) {
	files := []string{"/nonexistent/file.go"}
	_, tokens := EstimateUsage(files, map[string]bool{})
	if tokens <= 0 {
		t.Errorf("expected positive token estimate for one file, got %d", tokens)
	}
}
