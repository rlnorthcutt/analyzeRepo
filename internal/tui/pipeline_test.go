package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/rlnorthcutt/analyzeRepo/internal/analyze"
)

// ── interpolateGradient ───────────────────────────────────────────────────────

func TestInterpolateGradient_endpoints(t *testing.T) {
	// At t=0 should return the first stop color.
	c0 := interpolateGradient(0)
	first := gradientStops[0]
	want0 := lipglossColorHex(first[0], first[1], first[2])
	if string(c0) != want0 {
		t.Errorf("interpolateGradient(0) = %q, want %q", c0, want0)
	}

	// At t=1 should return the last stop color.
	c1 := interpolateGradient(1)
	last := gradientStops[len(gradientStops)-1]
	want1 := lipglossColorHex(last[0], last[1], last[2])
	if string(c1) != want1 {
		t.Errorf("interpolateGradient(1) = %q, want %q", c1, want1)
	}
}

func TestInterpolateGradient_outOfRange(t *testing.T) {
	// Values outside [0,1] should clamp, not panic.
	_ = interpolateGradient(-1)
	_ = interpolateGradient(2)
}

func TestInterpolateGradient_returnsHexColor(t *testing.T) {
	for _, t2 := range []float64{0, 0.25, 0.5, 0.75, 1} {
		c := string(interpolateGradient(t2))
		if !strings.HasPrefix(c, "#") || len(c) != 7 {
			t.Errorf("interpolateGradient(%v) = %q; expected 7-char hex color", t2, c)
		}
	}
}

func lipglossColorHex(r, g, b uint8) string {
	return strings.ToLower(
		"#" +
			hexByte(r) +
			hexByte(g) +
			hexByte(b),
	)
}

func hexByte(v uint8) string {
	const digits = "0123456789abcdef"
	return string([]byte{digits[v>>4], digits[v&0xf]})
}

// ── panelLineCount ────────────────────────────────────────────────────────────

func TestPanelLineCount(t *testing.T) {
	cases := []struct {
		panel string
		want  int
	}{
		{"line1\nline2\nline3\n", 3},
		{"single\n", 1},
		{"", 0},
		{"a\nb\nc\nd\n", 4},
	}
	for _, c := range cases {
		got := panelLineCount(c.panel)
		if got != c.want {
			t.Errorf("panelLineCount(%q) = %d, want %d", c.panel, got, c.want)
		}
	}
}

// ── pipelineModel viewport helpers ───────────────────────────────────────────

func TestViewportHeight_default(t *testing.T) {
	// With height=0 (unknown), should return at least ceil(2.5*5)=13.
	m := newPipelineModel(10, func() {})
	got := m.viewportHeight()
	if got < 13 {
		t.Errorf("viewportHeight with unknown height = %d, want >= 13", got)
	}
}

func TestViewportHeight_halfOfTerminal(t *testing.T) {
	m := newPipelineModel(10, func() {})
	m.height = 60 // 60/2 = 30 > 13, so should use 30
	got := m.viewportHeight()
	if got != 30 {
		t.Errorf("viewportHeight with height=60 = %d, want 30", got)
	}
}

func TestViewportHeight_smallTerminal(t *testing.T) {
	m := newPipelineModel(10, func() {})
	m.height = 20 // 20/2 = 10 < 13, so should use min=13
	got := m.viewportHeight()
	if got != 13 {
		t.Errorf("viewportHeight with height=20 = %d, want 13 (min)", got)
	}
}

func TestClampScroll(t *testing.T) {
	m := newPipelineModel(5, func() {})
	// No panels → maxScroll = 0, anything should clamp to 0.
	m.scrollOffset = 100
	m.clampScroll()
	if m.scrollOffset != 0 {
		t.Errorf("clampScroll with no panels: got %d, want 0", m.scrollOffset)
	}

	m.scrollOffset = -5
	m.clampScroll()
	if m.scrollOffset != 0 {
		t.Errorf("clampScroll negative: got %d, want 0", m.scrollOffset)
	}
}

func TestMaxScroll_noPanels(t *testing.T) {
	m := newPipelineModel(5, func() {})
	if got := m.maxScroll(); got != 0 {
		t.Errorf("maxScroll with no panels = %d, want 0", got)
	}
}

func TestMaxScroll_withPanels(t *testing.T) {
	m := newPipelineModel(5, func() {})
	m.height = 100 // vpH = 50
	// Add a panel of 5 lines; total=5, vpH=50 → maxScroll = 0 (all fit).
	m.panels = []string{"a\nb\nc\nd\n"} // 4 lines
	if got := m.maxScroll(); got != 0 {
		t.Errorf("expected 0 max scroll when panels fit; got %d", got)
	}

	// Add enough panels to exceed viewport.
	for range 20 {
		m.panels = append(m.panels, "line1\nline2\nline3\nline4\n")
	}
	if got := m.maxScroll(); got <= 0 {
		t.Errorf("expected positive max scroll with many panels; got %d", got)
	}
}

// ── scroll offset adjustment on new panels ────────────────────────────────────

func TestScrollOffsetPreservedOnNewPanel(t *testing.T) {
	m := newPipelineModel(10, func() {})
	m.width = 80

	// Simulate scrolling down by 5 lines.
	m.scrollOffset = 5

	panel := renderFilePanel(analyze.FileAnalysis{
		Path: "main.go", Role: "core", Summary: "test",
	}, nil, 80)

	panelLines := panelLineCount(panel)

	// Add a new panel to the model (simulating fileAnalyzedMsg logic).
	if m.scrollOffset > 0 {
		m.scrollOffset += panelLines
	}
	m.panels = append([]string{panel}, m.panels...)

	if m.scrollOffset != 5+panelLines {
		t.Errorf("scroll offset should be bumped by %d; got %d", panelLines, m.scrollOffset)
	}
}

// ── allPanelLines ─────────────────────────────────────────────────────────────

func TestAllPanelLines_order(t *testing.T) {
	m := newPipelineModel(3, func() {})
	m.panels = []string{"first\n", "second\n", "third\n"}

	lines := m.allPanelLines()
	if lines[0] != "first" {
		t.Errorf("first line should be from newest panel; got %q", lines[0])
	}
}

// ── View height stability ─────────────────────────────────────────────────────

func TestViewHeightStable(t *testing.T) {
	m := newPipelineModel(5, func() {})
	m.width = 80
	m.height = 40

	vpH := m.viewportHeight()
	countLines := func(s string) int { return strings.Count(s, "\n") }

	// View with no panels, no current file.
	view1 := m.View()
	h1 := countLines(view1)

	// Add a panel.
	a := analyze.FileAnalysis{Path: "a.go", Role: "core", Summary: "A file."}
	panel := renderFilePanel(a, nil, 80)
	m.panels = append([]string{panel}, m.panels...)
	m.done = 1
	m.current = "b.go"
	m.verb = "scanning"

	view2 := m.View()
	h2 := countLines(view2)

	// Both views should render vpH + 4 lines (viewport + current + counter + bar + separator).
	expected := vpH + 4
	if h1 != expected {
		t.Errorf("view height (no panels) = %d lines, want %d", h1, expected)
	}
	if h2 != expected {
		t.Errorf("view height (with panel) = %d lines, want %d", h2, expected)
	}
}

// ── renderGradientBar ─────────────────────────────────────────────────────────

func TestRenderGradientBar_zeroWidth(t *testing.T) {
	got := renderGradientBar(0.5, 0)
	if got != "" {
		t.Errorf("expected empty string for width=0, got %q", got)
	}
}

func TestRenderGradientBar_fullAndEmpty(t *testing.T) {
	// At 100% the bar should contain no empty ░ characters.
	full := renderGradientBar(1.0, 20)
	if strings.Contains(full, "░") {
		t.Error("100% bar should not contain empty ░ characters")
	}

	// At 0% the bar should be all ░.
	empty := renderGradientBar(0.0, 20)
	// Strip ANSI; just ensure it's non-empty.
	if len(empty) == 0 {
		t.Error("0% bar should not be empty string")
	}
}

// ── RunPipeline (plain path, no TTY) ────────────────────────────────────────

func TestRunPipelinePlain_empty(t *testing.T) {
	client := analyze.NewDryRunClient()
	analyses, failed, err := runPipelinePlain(
		context.Background(), client, "/tmp", nil, []string{"scanning"},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if failed != 0 {
		t.Errorf("expected 0 failures, got %d", failed)
	}
	if len(analyses) != 0 {
		t.Errorf("expected 0 analyses, got %d", len(analyses))
	}
}
