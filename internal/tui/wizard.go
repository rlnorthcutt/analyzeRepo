// Package tui provides Bubble Tea UI components for analyzerepo:
//   - Wizard: an interactive multi-step config collector
//   - Pipeline: a live progress view for the file analysis phase
package tui

import (
	"fmt"
	"maps"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

// ── Banner ────────────────────────────────────────────────────────────────────

// renderGradientBanner draws the analyzerepo title box with a 30° diagonal
// gradient border using the same colour stops as the progress bar.
// Each border character is coloured individually based on its (x, y) position
// projected onto the gradient axis: t = (x·cos30° + y·sin30°) / max_projection.
func renderGradientBanner() string {
	const (
		padH = 6 // horizontal padding chars per side
		padV = 1 // vertical padding lines per side
	)

	titleStr := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("255")).Render("analyzerepo")
	subtitleStr := dimStyle.Render("Onboard any codebase with Claude AI")

	// Visible width of the wider line determines the content column count.
	contentW := lipgloss.Width(titleStr)
	if w := lipgloss.Width(subtitleStr); w > contentW {
		contentW = w
	}

	innerW := contentW + padH*2 // visible chars between the two │ chars
	boxW := innerW + 2          // total box width including border chars

	// Build each interior row: padding rows + content rows + padding rows.
	blank := strings.Repeat(" ", innerW)
	padLeft := strings.Repeat(" ", padH)

	titleRow := padLeft + lipgloss.PlaceHorizontal(contentW, lipgloss.Center, titleStr) + strings.Repeat(" ", padH)
	subtitleRow := padLeft + lipgloss.PlaceHorizontal(contentW, lipgloss.Center, subtitleStr) + strings.Repeat(" ", padH)

	interior := []string{}
	for range padV {
		interior = append(interior, blank)
	}
	interior = append(interior, titleRow, subtitleRow)
	for range padV {
		interior = append(interior, blank)
	}

	boxH := len(interior) + 2 // +2 for top and bottom border rows

	// borderColor returns the interpolated gradient colour for border position (x, y).
	// cos(30°) ≈ 0.866, sin(30°) ≈ 0.5
	borderColor := func(x, y int) lipgloss.Color {
		maxProj := float64(boxW-1)*0.866 + float64(boxH-1)*0.5
		if maxProj == 0 {
			return interpolateGradient(0)
		}
		return interpolateGradient((float64(x)*0.866 + float64(y)*0.5) / maxProj)
	}
	cell := func(x, y int, ch string) string {
		return lipgloss.NewStyle().Foreground(borderColor(x, y)).Render(ch)
	}

	var sb strings.Builder

	// Top border row (y = 0).
	sb.WriteString(cell(0, 0, "╭"))
	for x := 1; x < boxW-1; x++ {
		sb.WriteString(cell(x, 0, "─"))
	}
	sb.WriteString(cell(boxW-1, 0, "╮"))
	sb.WriteString("\n")

	// Interior rows.
	for i, row := range interior {
		y := i + 1
		sb.WriteString(cell(0, y, "│"))
		sb.WriteString(row)
		sb.WriteString(cell(boxW-1, y, "│"))
		sb.WriteString("\n")
	}

	// Bottom border row (y = boxH-1).
	sb.WriteString(cell(0, boxH-1, "╰"))
	for x := 1; x < boxW-1; x++ {
		sb.WriteString(cell(x, boxH-1, "─"))
	}
	sb.WriteString(cell(boxW-1, boxH-1, "╯"))

	return sb.String()
}

// PrintBanner prints the analyzerepo title block to stdout.
// Called once at the start of every run so the banner is visible in normal CLI
// output — not just inside the wizard's alt-screen.
func PrintBanner() {
	width := termWidth()
	fmt.Println(lipgloss.PlaceHorizontal(width, lipgloss.Center, renderGradientBanner()))
}

// termWidth returns the current terminal width, defaulting to 80.
func termWidth() int {
	if w, _, err := term.GetSize(1); err == nil && w > 0 {
		return w
	}
	return 80
}

// ── Shared styles ─────────────────────────────────────────────────────────────

var (
	primaryColor = lipgloss.Color("63")  // blue
	successColor = lipgloss.Color("42")  // green
	dimColor     = lipgloss.Color("240") // gray

	labelStyle = lipgloss.NewStyle().Bold(true)
	dimStyle   = lipgloss.NewStyle().Foreground(dimColor)
	checkStyle = lipgloss.NewStyle().Foreground(successColor).Bold(true)

	reportPanelStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(primaryColor).
				Padding(1, 2)
)

// ── Config ────────────────────────────────────────────────────────────────────

// Config holds the configuration collected by the wizard (or from CLI flags).
type Config struct {
	Source    string
	Full      bool
	Reports   map[string]bool
	OutputDir string
}

// ── Report definitions ────────────────────────────────────────────────────────

type reportDef struct {
	key, name, file, desc string
}

var reportDefs = []reportDef{
	{"onboarding", "Onboarding Guide", "ONBOARDING.md",
		"Executive summary, language stats, and file tree"},
	{"improvement", "Code Analysis", "ANALYSIS.md",
		"Per-file role summaries and improvement suggestions"},
	{"claude", "CLAUDE.md", "CLAUDE.md",
		"AI assistant context file for use with Claude Code"},
}

// ── Wizard model ──────────────────────────────────────────────────────────────

type wizardStep int

const (
	stepSource wizardStep = iota
	stepReports
	stepFull
	stepOutput
	stepDone
)

// wizardModel is the Bubble Tea model for the interactive configuration wizard.
type wizardModel struct {
	step        wizardStep
	sourceInput textinput.Model
	toggleInput textinput.Model // shared for reports number entry and full y/n
	outputInput textinput.Model

	selection map[string]bool // current report selection

	// skip flags: skip this step because the value was already provided via flag/arg
	skipSource  bool
	skipReports bool
	skipFull    bool
	skipOutput  bool

	// initCmd focuses the correct input when the wizard opens at a non-source step.
	initCmd tea.Cmd

	width  int
	err    error
	Result Config
}

func newWizardModel(pre Config, skipSource, skipReports, skipFull, skipOutput bool) wizardModel {
	si := textinput.New()
	si.Placeholder = "https://github.com/owner/repo  or  ./my-project"
	if pre.Source != "" {
		si.SetValue(pre.Source)
	}

	ti := textinput.New()
	ti.CharLimit = 20

	oi := textinput.New()
	oi.Placeholder = "."
	oi.SetValue(".")
	if pre.OutputDir != "" {
		oi.SetValue(pre.OutputDir)
	}

	sel := map[string]bool{"onboarding": true, "improvement": true, "claude": true}
	if len(pre.Reports) > 0 {
		sel = pre.Reports
	}

	m := wizardModel{
		step:        stepSource,
		sourceInput: si,
		toggleInput: ti,
		outputInput: oi,
		selection:   sel,
		skipSource:  skipSource,
		skipReports: skipReports,
		skipFull:    skipFull,
		skipOutput:  skipOutput,
		Result: Config{
			Source:    pre.Source,
			Reports:   sel,
			Full:      pre.Full,
			OutputDir: pre.OutputDir,
		},
	}

	// Find the first step that isn't skipped and set up its input focus.
	for m.step < stepDone && m.shouldSkip(m.step) {
		m.step++
	}
	switch m.step {
	case stepSource:
		si.Focus() // sourceInput was already assigned above; re-focus here
		m.sourceInput = si
	case stepReports:
		ti.Placeholder = ""
		m.initCmd = ti.Focus()
		m.toggleInput = ti
	case stepFull:
		ti.Placeholder = "y/N"
		m.initCmd = ti.Focus()
		m.toggleInput = ti
	case stepOutput:
		m.initCmd = oi.Focus()
		m.outputInput = oi
	}

	return m
}

// RunWizard launches the full-screen interactive wizard and returns the completed Config.
// pre contains any values already set by CLI flags/args. skip* flags indicate which
// steps to bypass because the value is already provided.
func RunWizard(pre Config, skipSource, skipReports, skipFull, skipOutput bool) (Config, error) {
	m := newWizardModel(pre, skipSource, skipReports, skipFull, skipOutput)
	if m.step == stepDone {
		// All steps skipped — nothing to ask.
		return m.Result, nil
	}
	p := tea.NewProgram(m, tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return Config{}, err
	}
	wm := final.(wizardModel)
	if wm.err != nil {
		return Config{}, wm.err
	}
	return wm.Result, nil
}

// ── Bubble Tea interface ──────────────────────────────────────────────────────

func (m wizardModel) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, m.initCmd)
}

func (m wizardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.err = fmt.Errorf("interrupted")
			return m, tea.Quit
		case "enter":
			return m.handleEnter()
		}

		// At the reports step, digit keys toggle immediately.
		// Any other key (arrows, letters, etc.) is silently ignored.
		if m.step == stepReports {
			k := msg.String()
			if len(k) == 1 && k >= "1" && k <= "9" {
				m.toggleReport(k)
			}
			return m, nil
		}
	}

	// Route key events to the active input.
	var cmd tea.Cmd
	switch m.step {
	case stepSource:
		m.sourceInput, cmd = m.sourceInput.Update(msg)
	case stepFull:
		m.toggleInput, cmd = m.toggleInput.Update(msg)
	case stepOutput:
		m.outputInput, cmd = m.outputInput.Update(msg)
	}
	return m, cmd
}

// handleEnter processes the Enter key for the current step.
func (m wizardModel) handleEnter() (tea.Model, tea.Cmd) {
	switch m.step {

	case stepSource:
		src := strings.TrimSpace(m.sourceInput.Value())
		if src == "" {
			return m, nil // require non-empty
		}
		m.Result.Source = src
		return m.advance()

	case stepReports:
		// Enter confirms whatever is currently checked.
		m.Result.Reports = make(map[string]bool, len(m.selection))
		maps.Copy(m.Result.Reports, m.selection)
		return m.advance()

	case stepFull:
		val := strings.ToLower(strings.TrimSpace(m.toggleInput.Value()))
		m.Result.Full = val == "y" || val == "yes"
		m.toggleInput.SetValue("")
		return m.advance()

	case stepOutput:
		out := strings.TrimSpace(m.outputInput.Value())
		if out == "" {
			out = "."
		}
		m.Result.OutputDir = out
		return m.advance()
	}

	return m, nil
}

// toggleReport flips the selection for the report matching token (number or key name).
func (m *wizardModel) toggleReport(token string) {
	var key string
	if n, err := strconv.Atoi(token); err == nil && n >= 1 && n <= len(reportDefs) {
		key = reportDefs[n-1].key
	} else {
		for _, r := range reportDefs {
			if strings.EqualFold(token, r.key) {
				key = r.key
				break
			}
		}
	}
	if key == "" {
		return
	}
	candidate := make(map[string]bool, len(m.selection))
	maps.Copy(candidate, m.selection)
	if candidate[key] {
		delete(candidate, key)
	} else {
		candidate[key] = true
	}
	if len(candidate) > 0 { // never allow empty selection
		m.selection = candidate
	}
}

// advance moves to the next non-skipped step and sets up the input for it.
func (m wizardModel) advance() (tea.Model, tea.Cmd) {
	for {
		m.step++
		if m.step >= stepDone {
			m.step = stepDone
			return m, tea.Quit
		}
		if !m.shouldSkip(m.step) {
			break
		}
	}
	return m.prepareStep()
}

func (m wizardModel) shouldSkip(step wizardStep) bool {
	switch step {
	case stepSource:
		return m.skipSource
	case stepReports:
		return m.skipReports
	case stepFull:
		return m.skipFull
	case stepOutput:
		return m.skipOutput
	}
	return false
}

// prepareStep blurs all inputs, then focuses the correct one for the current step.
func (m wizardModel) prepareStep() (tea.Model, tea.Cmd) {
	m.sourceInput.Blur()
	m.toggleInput.Blur()
	m.outputInput.Blur()
	m.toggleInput.SetValue("")

	switch m.step {
	case stepSource:
		return m, m.sourceInput.Focus()
	case stepReports:
		m.toggleInput.Placeholder = ""
		return m, m.toggleInput.Focus()
	case stepFull:
		m.toggleInput.Placeholder = "N/y"
		return m, m.toggleInput.Focus()
	case stepOutput:
		return m, m.outputInput.Focus()
	}
	return m, nil
}

// ── View ──────────────────────────────────────────────────────────────────────

func (m wizardModel) View() string {
	if m.step == stepDone {
		return ""
	}

	width := m.width
	if width < 40 {
		width = 80
	}

	var sb strings.Builder

	// Banner — centered horizontally.
	sb.WriteString(lipgloss.PlaceHorizontal(width, lipgloss.Center, renderGradientBanner()))
	sb.WriteString("\n\n")

	// Current step content.
	switch m.step {
	case stepSource:
		sb.WriteString(wizardRule("Repository", width))
		sb.WriteString("\n\n")
		sb.WriteString("  " + labelStyle.Render("GitHub URL or local path") + "\n\n")
		sb.WriteString("  " + m.sourceInput.View() + "\n")

	case stepReports:
		sb.WriteString(wizardRule("Reports", width))
		sb.WriteString("\n\n")
		sb.WriteString(m.renderReportsPanel(width))
		sb.WriteString("\n")

	case stepFull:
		sb.WriteString(wizardRule("Analysis Scope", width))
		sb.WriteString("\n\n")
		sb.WriteString("  " + labelStyle.Render("By default, Claude will select key files to review. Analyze every file instead?") + "\n")
		sb.WriteString("  " + dimStyle.Render("default: Claude selects key files") + "\n\n")
		sb.WriteString("  " + dimStyle.Render("[N/y]: ") + m.toggleInput.View() + "\n")

	case stepOutput:
		sb.WriteString(wizardRule("Output", width))
		sb.WriteString("\n\n")
		sb.WriteString("  " + labelStyle.Render("Output directory") + "\n\n")
		sb.WriteString("  " + m.outputInput.View() + "\n")
	}

	return sb.String()
}

// renderReportsPanel renders the numbered toggle list for the reports step.
func (m wizardModel) renderReportsPanel(width int) string {
	var lines []string
	for i, r := range reportDefs {
		num := labelStyle.Render(strconv.Itoa(i + 1))
		var check, nameText string
		if m.selection[r.key] {
			check = checkStyle.Render("✓")
			nameText = labelStyle.Render(r.name)
		} else {
			check = dimStyle.Render("·")
			nameText = dimStyle.Render(r.name)
		}
		lines = append(lines,
			fmt.Sprintf("  %s  %s  %s  %s", num, check, nameText, dimStyle.Render(r.file)),
		)
		lines = append(lines, "          "+dimStyle.Render(r.desc))
	}

	subtitle := dimStyle.Render("press 1 / 2 / 3 to toggle · Enter to confirm")
	return reportPanelStyle.
		Width(width-4).
		UnsetMaxWidth().
		Render(strings.Join(lines, "\n")) +
		"\n  " + subtitle
}

// wizardRule renders a horizontal rule with a centered title label.
func wizardRule(title string, width int) string {
	label := " " + dimStyle.Render(title) + " "
	sides := max(0, (width-lipgloss.Width(label)-2)/2)
	line := strings.Repeat("─", sides)
	return dimStyle.Render(line) + label + dimStyle.Render(line)
}
