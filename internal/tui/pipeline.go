package tui

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rlnorthcutt/analyzeRepo/internal/analyze"
)

// ── Role colors ───────────────────────────────────────────────────────────────

var roleColor = map[string]lipgloss.Color{
	"entrypoint": lipgloss.Color("69"),  // blue
	"core":       lipgloss.Color("135"), // purple
	"config":     lipgloss.Color("214"), // orange
	"test":       lipgloss.Color("82"),  // green
	"docs":       lipgloss.Color("39"),  // cyan
	"util":       lipgloss.Color("240"), // gray
	"data":       lipgloss.Color("220"), // yellow
	"build":      lipgloss.Color("203"), // red-orange
	"other":      lipgloss.Color("240"), // gray
}

// ── Progress bar gradient ─────────────────────────────────────────────────────

// gradientStops defines the 4-color progress bar gradient as RGB triples.
// Order: red → purple → dark blue → cyan.
var gradientStops = [4][3]uint8{
	{255, 95, 95},  // red       #ff5f5f
	{175, 95, 255}, // purple    #af5fff
	{0, 0, 175},    // dark blue #0000af
	{0, 175, 255},  // cyan      #00afff
}

// interpolateGradient returns the hex lipgloss.Color at position t ∈ [0, 1]
// across the four gradient stops using linear RGB interpolation.
func interpolateGradient(t float64) lipgloss.Color {
	n := len(gradientStops)
	if t <= 0 {
		c := gradientStops[0]
		return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", c[0], c[1], c[2]))
	}
	if t >= 1 {
		c := gradientStops[n-1]
		return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", c[0], c[1], c[2]))
	}
	pos := t * float64(n-1) // map t to segment space
	i := int(pos)
	if i >= n-1 {
		i = n - 2
	}
	local := pos - float64(i)
	a, b := gradientStops[i], gradientStops[i+1]
	r := uint8(math.Round(float64(a[0])*(1-local) + float64(b[0])*local))
	g := uint8(math.Round(float64(a[1])*(1-local) + float64(b[1])*local))
	bv := uint8(math.Round(float64(a[2])*(1-local) + float64(b[2])*local))
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", r, g, bv))
}

// renderGradientBar renders a terminal progress bar of the given width.
// The filled portion reveals the 4-stop gradient left-to-right; the empty
// portion is rendered as dim ░ characters.
func renderGradientBar(pct float64, width int) string {
	if width <= 0 {
		return ""
	}
	filled := int(math.Round(pct * float64(width)))
	if filled > width {
		filled = width
	}
	var sb strings.Builder
	for i := 0; i < filled; i++ {
		var t float64
		if width > 1 {
			t = float64(i) / float64(width-1)
		}
		sb.WriteString(lipgloss.NewStyle().Background(interpolateGradient(t)).Render(" "))
	}
	if empty := width - filled; empty > 0 {
		sb.WriteString(dimStyle.Render(strings.Repeat("░", empty)))
	}
	return sb.String()
}

// ── Message types ─────────────────────────────────────────────────────────────

// fileAnalyzedMsg is sent when a file finishes analysis (success or failure).
type fileAnalyzedMsg struct {
	analysis analyze.FileAnalysis
	err      error
}

// currentFileMsg is sent just before a file is analysed, so the UI can show
// which file is in flight.
type currentFileMsg struct {
	index int
	path  string
	verb  string
}

// pipelineDoneMsg signals that all goroutine work is complete.
type pipelineDoneMsg struct{}

// progTickMsg drives the smooth progress bar easing animation.
type progTickMsg struct{}

func progTick() tea.Cmd {
	return tea.Tick(33*time.Millisecond, func(time.Time) tea.Msg { return progTickMsg{} })
}

// ── Pipeline model ────────────────────────────────────────────────────────────

// pipelineModel is the Bubble Tea model for the live file-analysis progress view.
type pipelineModel struct {
	progPct     float64            // current animated percentage (0.0–1.0)
	progTarget  float64            // target percentage (done/total)
	progTicking bool               // true while a progTick is in flight
	total       int
	done        int
	failed      int
	current     string             // file path being analysed right now
	verb        string             // flavour verb for current file
	analyses    []analyze.FileAnalysis
	finished    bool
	userQuit    bool               // true when the user pressed Ctrl+C
	cancel      context.CancelFunc // cancels the analysis goroutine
	width       int
}

func newPipelineModel(total int, cancel context.CancelFunc) pipelineModel {
	return pipelineModel{
		total:    total,
		analyses: make([]analyze.FileAnalysis, 0, total),
		cancel:   cancel,
	}
}

// ── Bubble Tea interface ──────────────────────────────────────────────────────

func (m pipelineModel) Init() tea.Cmd { return nil }

func (m pipelineModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			if m.cancel != nil {
				m.cancel()
			}
			m.userQuit = true
			return m, tea.Quit
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil

	case currentFileMsg:
		m.current = msg.path
		m.verb = msg.verb
		return m, nil

	case fileAnalyzedMsg:
		if msg.err != nil {
			m.failed++
			m.analyses = append(m.analyses, analyze.FileAnalysis{
				Path: msg.analysis.Path, Role: "other", Summary: "Analysis failed.",
			})
		} else {
			m.analyses = append(m.analyses, msg.analysis)
		}
		m.done++

		// Print a persistent panel above the progress bar.
		panel := renderFilePanel(msg.analysis, msg.err, m.width)
		cmd := tea.Printf("%s", panel)

		// Advance the progress target and start the easing animation if idle.
		m.progTarget = float64(m.done) / float64(m.total)
		if !m.progTicking {
			m.progTicking = true
			return m, tea.Batch(cmd, progTick())
		}
		return m, cmd

	case pipelineDoneMsg:
		m.finished = true
		return m, tea.Quit

	case progTickMsg:
		if m.progPct < m.progTarget {
			step := (m.progTarget - m.progPct) * 0.18
			if step < 0.002 {
				m.progPct = m.progTarget
			} else {
				m.progPct += step
				return m, progTick() // keep easing
			}
		}
		m.progTicking = false
	}

	return m, nil
}

func (m pipelineModel) View() string {
	width := m.width
	if width < 40 {
		width = 80
	}

	// When finished, leave the completed bar on screen so it persists while
	// reports are being written.
	if m.finished {
		return "  " + renderGradientBar(1.0, width-4) + "\n"
	}

	var sb strings.Builder

	// Current file indicator.
	if m.current != "" {
		verb := dimStyle.Render(m.verb)
		file := labelStyle.Render(m.current)
		sb.WriteString(fmt.Sprintf("  %s %s\n", verb, file))
	}

	// Counter.
	counter := dimStyle.Render(fmt.Sprintf("  %d / %d files", m.done, m.total))
	sb.WriteString(counter + "\n")

	// Gradient progress bar.
	sb.WriteString("  " + renderGradientBar(m.progPct, width-4) + "\n")

	return sb.String()
}

// ── File panel renderer ───────────────────────────────────────────────────────

// renderFilePanel returns a styled, persistent panel for one completed file.
// The border color matches the file's role color.
func renderFilePanel(a analyze.FileAnalysis, err error, width int) string {
	if width < 40 {
		width = 80
	}

	var header, body string
	var borderColor lipgloss.Color

	if err != nil {
		borderColor = lipgloss.Color("203") // red-orange for errors
		header = lipgloss.NewStyle().Foreground(borderColor).Bold(true).
			Render("✗ " + a.Path)
		body = dimStyle.Render("  analysis failed")
	} else {
		color, ok := roleColor[a.Role]
		if !ok {
			color = roleColor["other"]
		}
		borderColor = color
		roleTag := lipgloss.NewStyle().Foreground(color).Bold(true).
			Render("[" + a.Role + "]")
		header = checkStyle.Render("✓ ") + labelStyle.Render(filepath.Base(a.Path)) +
			"  " + roleTag
		// Truncate summary to one line.
		summary := a.Summary
		if idx := strings.IndexByte(summary, '\n'); idx != -1 {
			summary = summary[:idx]
		}
		const maxSummary = 120
		if len(summary) > maxSummary {
			summary = summary[:maxSummary] + "…"
		}
		body = dimStyle.Render("  " + summary)
	}

	panel := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(0, 1).
		Width(width - 4).
		UnsetMaxWidth()

	return panel.Render(header+"\n"+body) + "\n"
}

// ── RunPipeline ───────────────────────────────────────────────────────────────

// RunPipeline analyses each file in `files`, printing a live Bubble Tea
// progress view when stdout is a TTY, or plain line-by-line output otherwise.
// Returns all FileAnalysis results (including stubs for failures) and a count
// of failures.
func RunPipeline(
	ctx context.Context,
	client *analyze.Client,
	root string,
	files []string,
	verbs []string,
) (analyses []analyze.FileAnalysis, failed int, err error) {

	// Detect whether stdout is a real terminal. When it isn't (piped output,
	// CI, --non-interactive, etc.) we skip the Bubble Tea renderer and print
	// plain progress lines instead.
	stat, _ := os.Stdout.Stat()
	isTTY := (stat.Mode() & os.ModeCharDevice) != 0

	if !isTTY {
		return runPipelinePlain(ctx, client, root, files, verbs)
	}

	// Create a child context so the model can cancel the goroutine on Ctrl+C.
	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	m := newPipelineModel(len(files), cancel)
	p := tea.NewProgram(m) // no WithAltScreen — panels scroll naturally

	// Launch the analysis goroutine; it sends messages to the Bubble Tea program.
	go func() {
		for i, abs := range files {
			// Stop immediately if the context was cancelled (Ctrl+C or parent).
			if childCtx.Err() != nil {
				p.Send(pipelineDoneMsg{})
				return
			}

			rel, _ := filepath.Rel(root, abs)
			verb := verbs[i%len(verbs)]

			p.Send(currentFileMsg{index: i, path: rel, verb: verb})

			a, analysisErr := analyze.AnalyzeFile(childCtx, client, root, rel)
			if analysisErr != nil {
				a = analyze.FileAnalysis{Path: rel}
			}
			p.Send(fileAnalyzedMsg{analysis: a, err: analysisErr})
		}
		p.Send(pipelineDoneMsg{})
	}()

	final, runErr := p.Run()
	if runErr != nil {
		return nil, 0, runErr
	}

	fm := final.(pipelineModel)
	if fm.userQuit {
		return fm.analyses, fm.failed, fmt.Errorf("interrupted")
	}
	return fm.analyses, fm.failed, nil
}

// runPipelinePlain is the non-TTY fallback: analyses files sequentially and
// prints a simple "[n/total] verb path" line per file.
func runPipelinePlain(
	ctx context.Context,
	client *analyze.Client,
	root string,
	files []string,
	verbs []string,
) ([]analyze.FileAnalysis, int, error) {
	analyses := make([]analyze.FileAnalysis, 0, len(files))
	failed := 0
	for i, abs := range files {
		if ctx.Err() != nil {
			return analyses, failed, ctx.Err()
		}

		rel, _ := filepath.Rel(root, abs)
		verb := verbs[i%len(verbs)]
		fmt.Fprintf(os.Stdout, "  [%d/%d] %s %s\n", i+1, len(files), verb, rel)

		a, analysisErr := analyze.AnalyzeFile(ctx, client, root, rel)
		if analysisErr != nil {
			fmt.Fprintf(os.Stdout, "         warning: %v\n", analysisErr)
			a = analyze.FileAnalysis{Path: rel, Role: "other", Summary: "Analysis failed."}
			failed++
		}
		analyses = append(analyses, a)
	}
	return analyses, failed, nil
}
