# CLAUDE.md

# analyzeRepo

A Go CLI tool that analyzes GitHub repositories or local codebases using Claude AI to generate onboarding guides, per-file analysis with improvement suggestions, and CLAUDE.md context files.

## Architecture

The tool is structured as a standard Go CLI application using Cobra, with all business logic in `internal/` packages:

- **cmd/root.go** wires together all packages, resolves config (interactive wizard or flags), and drives the analysis pipeline through five sequential phases: discovery, key file selection, per-file analysis, stats, and report writing.
- **internal/analyze** is the AI layer. It contains a dual-backend `Client` that uses the Anthropic SDK when `ANTHROPIC_API_KEY` is set, falling back to the `claude` CLI subprocess. Token usage is tracked atomically across concurrent goroutines.
- **internal/fetch** is the ingestion layer. It clones GitHub repos or resolves local paths, walks the file tree filtering ignored/binary files, and renders a directory tree string.
- **internal/report** is the output layer. Pure writer functions take structured data and produce `ANALYSIS.md`, `ONBOARDING.md`, and `CLAUDE.md` files.
- **internal/stats** computes per-language file and line counts from the file list.
- **internal/tui** contains two Bubble Tea models: a multi-step wizard for interactive config collection (`wizard.go`) and a live progress pipeline view with a scrollable results panel (`pipeline.go`).

## Directory Structure

```
main.go                  # Minimal bootstrap; calls cmd.Execute()
cmd/
  root.go                # Cobra command, flag parsing, pipeline orchestration
  root_test.go
internal/
  analyze/
    analyze.go           # Claude AI client, dual-backend, per-file & report prompts
    analyze_test.go
  fetch/
    fetch.go             # Repo cloning, file tree walking, binary detection
    fetch_test.go
  report/
    report.go            # Markdown report writers (ANALYSIS.md, ONBOARDING.md, CLAUDE.md)
    report_test.go
  stats/
    stats.go             # Language detection and line counting
    stats_test.go
  tui/
    pipeline.go          # Bubble Tea model for live analysis progress
    pipeline_test.go
    wizard.go            # Bubble Tea multi-step config wizard
go.mod
go.sum
```

## Development Conventions

- **Go module path**: `github.com/rlnorthcutt/analyzeRepo`
- **Package naming**: matches directory name; no abbreviations
- **Error handling**: return errors up the call stack; do not silently discard them (avoid `_ = err`)
- **Concurrency**: per-file analysis runs concurrently; use atomic operations for shared counters (see token tracking in `analyze.go`)
- **TUI**: all interactive UI goes in `internal/tui`; presentation helpers (ANSI, phase printing) belong there too, not in `cmd/root.go`
- **AI model**: use the Anthropic SDK constant (`anthropic.ModelClaudeOpus4_6`) as the single source of truth for the model name — do not duplicate it as a separate string constant

## Common Tasks

```bash
# Build
go build -o analyzeRepo .

# Run (interactive wizard)
./analyzeRepo

# Run with flags (non-interactive)
./analyzeRepo --source https://github.com/owner/repo --reports all

# Run on a local path
./analyzeRepo --source ./path/to/project --output ./out

# Run tests
go test ./...

# Run tests with verbose output
go test -v ./...
```

## Environment

- `ANTHROPIC_API_KEY` — when set, the tool uses the Anthropic SDK directly. When absent, it falls back to invoking the `claude` CLI subprocess.

## What to Avoid

- **Silent error discard**: do not use `_ = err` or `return nil` inside walk/error callbacks without at least logging — callers will get incomplete results with no indication.
- **Bare substring matching for error codes**: avoid matching on `"401"` alone; use `"status 401"` or the SDK error type to prevent false positives from URLs or other messages containing that string.
- **Git URL injection**: always validate the `source` argument against a strict regex (e.g., `^https://github\.com/[\w.-]+/[\w.-]+`) before passing to `git clone`. Even though `exec.Command` avoids shell injection, git flag injection via `--upload-pack=...` is still possible.
- **Duplicate model constants**: the SDK provides `anthropic.ModelClaudeOpus4_6`; do not define a parallel `const model = "claude-opus-4-6"` alongside it.
- **Raw byte truncation of file content**: truncating at a fixed byte offset can split UTF-8 characters. Truncate on a line boundary instead.
- **Global mutable flag variables**: package-level flag vars make concurrent or multi-command use unsafe. Prefer reading flag values via `cmd.Flags().GetString(...)` inside `RunE`.
- **Monolithic pipeline function**: the five analysis phases should be separate named functions, not one large `pipeline` function, to keep error handling isolated and the code readable.