package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rlnorthcutt/analyzeRepo/internal/analyze"
	"github.com/rlnorthcutt/analyzeRepo/internal/stats"
)

// ── capitalize ────────────────────────────────────────────────────────────────

func TestCapitalize(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"hello", "Hello"},
		{"HELLO", "HELLO"},
		{"core", "Core"},
		{"entrypoint", "Entrypoint"},
		{"", ""},
		{"a", "A"},
	}
	for _, c := range cases {
		got := capitalize(c.in)
		if got != c.want {
			t.Errorf("capitalize(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// ── guessMDPurpose ────────────────────────────────────────────────────────────

func TestGuessMDPurpose_knownStems(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"README.md", "Project overview and getting started guide"},
		{"CHANGELOG.md", "Changelog / release history"},
		{"CONTRIBUTING.md", "Contribution guidelines"},
		{"LICENSE.md", "License information"},
		{"CLAUDE.md", "AI coding assistant instructions"},
		{"ARCHITECTURE.md", "Architecture and design overview"},
		{"SECURITY.md", "Security policy"},
	}
	for _, c := range cases {
		got := guessMDPurpose(c.path)
		if got != c.want {
			t.Errorf("guessMDPurpose(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

func TestGuessMDPurpose_docsDirectory(t *testing.T) {
	// "docs/" prefix → "Documentation" only when the stem has no known mapping.
	got := guessMDPurpose("docs/something-unknown.md")
	if got != "Documentation" {
		t.Errorf("guessMDPurpose(%q) = %q, want %q", "docs/something-unknown.md", got, "Documentation")
	}
	// Files whose stem matches a known key return that key's description,
	// even when inside docs/.
	got = guessMDPurpose("docs/guide.md")
	if got == "" {
		t.Error("guessMDPurpose(docs/guide.md) returned empty")
	}
}

func TestGuessMDPurpose_unknownFile(t *testing.T) {
	// Unknown files default to "Documentation".
	got := guessMDPurpose("some-random-notes.md")
	if got != "Documentation" {
		t.Errorf("expected default 'Documentation', got %q", got)
	}
}

func TestGuessMDPurpose_hyphenAndCase(t *testing.T) {
	// Stems with hyphens/underscores and mixed case should still match.
	got := guessMDPurpose("getting-started.md")
	if got == "" {
		t.Error("expected non-empty result for getting-started.md")
	}
}

// ── buildMDLibrary ────────────────────────────────────────────────────────────

func TestBuildMDLibrary_empty(t *testing.T) {
	got := buildMDLibrary(nil)
	if got != "" {
		t.Errorf("expected empty string for nil input, got %q", got)
	}
}

func TestBuildMDLibrary_outputStructure(t *testing.T) {
	files := []string{"README.md", "CHANGELOG.md"}
	got := buildMDLibrary(files)

	if !strings.Contains(got, "## Documentation Library") {
		t.Error("missing section header")
	}
	if !strings.Contains(got, "README.md") {
		t.Error("missing README.md")
	}
	if !strings.Contains(got, "CHANGELOG.md") {
		t.Error("missing CHANGELOG.md")
	}
}

func TestBuildMDLibrary_sorted(t *testing.T) {
	// Output should be alphabetically sorted regardless of input order.
	files := []string{"Z.md", "A.md", "M.md"}
	got := buildMDLibrary(files)

	aIdx := strings.Index(got, "A.md")
	mIdx := strings.Index(got, "M.md")
	zIdx := strings.Index(got, "Z.md")

	if !(aIdx < mIdx && mIdx < zIdx) {
		t.Errorf("expected alphabetical order A < M < Z; positions: A=%d M=%d Z=%d", aIdx, mIdx, zIdx)
	}
}

// ── WriteAnalysis ─────────────────────────────────────────────────────────────

func TestWriteAnalysis_createsFile(t *testing.T) {
	dir := t.TempDir()
	analyses := []analyze.FileAnalysis{
		{
			Path:    "cmd/main.go",
			Role:    "entrypoint",
			Summary: "Main entry point of the application.",
			Suggestions: []analyze.Suggestion{
				{Type: "improvement", Description: "Add error handling.", Effort: "low"},
			},
		},
	}

	outPath, err := WriteAnalysis(analyses, dir)
	if err != nil {
		t.Fatalf("WriteAnalysis returned error: %v", err)
	}
	if outPath != filepath.Join(dir, "ANALYSIS.md") {
		t.Errorf("unexpected output path %q", outPath)
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("could not read output file: %v", err)
	}
	body := string(content)

	if !strings.Contains(body, "cmd/main.go") {
		t.Error("output missing file path")
	}
	if !strings.Contains(body, "Main entry point") {
		t.Error("output missing summary text")
	}
	if !strings.Contains(body, "improvement") {
		t.Error("output missing suggestion type")
	}
	if !strings.Contains(body, "Entrypoint") {
		t.Error("output missing role heading")
	}
}

func TestWriteAnalysis_roleGrouping(t *testing.T) {
	dir := t.TempDir()
	analyses := []analyze.FileAnalysis{
		{Path: "main.go", Role: "entrypoint", Summary: "Entry."},
		{Path: "utils.go", Role: "util", Summary: "Utilities."},
		{Path: "config.go", Role: "config", Summary: "Config."},
	}

	outPath, err := WriteAnalysis(analyses, dir)
	if err != nil {
		t.Fatalf("WriteAnalysis error: %v", err)
	}
	content := string(must(os.ReadFile(outPath)))

	// Role sections should appear in roleOrder (entrypoint before config before util).
	entryIdx := strings.Index(content, "Entrypoint")
	configIdx := strings.Index(content, "Config")
	utilIdx := strings.Index(content, "Util")

	if !(entryIdx < configIdx && configIdx < utilIdx) {
		t.Errorf("roles out of order: entrypoint=%d config=%d util=%d", entryIdx, configIdx, utilIdx)
	}
}

// ── WriteOnboarding ───────────────────────────────────────────────────────────

func TestWriteOnboarding_createsFile(t *testing.T) {
	dir := t.TempDir()
	st := stats.Stats{
		TotalFiles: 2,
		TotalLines: 50,
		Languages:  map[string]stats.LangStats{"Go": {Files: 2, Lines: 50}},
	}

	outPath, err := WriteOnboarding("Executive summary.", st, "root/\n  main.go", "myrepo", dir, []string{"README.md"})
	if err != nil {
		t.Fatalf("WriteOnboarding error: %v", err)
	}

	content := string(must(os.ReadFile(outPath)))

	if !strings.Contains(content, "myrepo") {
		t.Error("missing repo name")
	}
	if !strings.Contains(content, "Executive summary.") {
		t.Error("missing summary")
	}
	if !strings.Contains(content, "README.md") {
		t.Error("missing MD library entry")
	}
	if !strings.Contains(content, "main.go") {
		t.Error("missing file tree")
	}
}

// ── WriteClaudeMD ─────────────────────────────────────────────────────────────

func TestWriteClaudeMD_createsFile(t *testing.T) {
	dir := t.TempDir()
	content := "# CLAUDE.md\n\nSome instructions."

	outPath, err := WriteClaudeMD(content, dir)
	if err != nil {
		t.Fatalf("WriteClaudeMD error: %v", err)
	}

	got := string(must(os.ReadFile(outPath)))
	if got != content {
		t.Errorf("file content mismatch:\ngot:  %q\nwant: %q", got, content)
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func must(b []byte, err error) []byte {
	if err != nil {
		panic(err)
	}
	return b
}
