# analyzeRepo — Onboarding Guide

## Purpose

`analyzeRepo` is a CLI tool that analyzes GitHub repositories or local codebases using Claude AI to produce three types of output: a per-file analysis with improvement suggestions (`ANALYSIS.md`), a developer onboarding guide (`ONBOARDING.md`), and a `CLAUDE.md` context file suitable for use with Claude Code. The tool bridges repository inspection with AI-powered documentation generation, making it useful for quickly understanding unfamiliar codebases or generating AI-ready context files.

## Architecture

The project follows a standard Go CLI layout with a thin `main.go` bootstrapping a Cobra command defined in `cmd/root.go`. Business logic is fully contained in `internal/`, split into focused packages:

- **`internal/fetch`** — Repository acquisition (GitHub clone or local path resolution) and filesystem traversal with filtering of binary/ignored files. This is the ingestion layer.
- **`internal/analyze`** — All Claude AI interaction. Implements a dual-backend `Client` that prefers the Anthropic Go SDK when `ANTHROPIC_API_KEY` is set, falling back to invoking the `claude` CLI as a subprocess. Handles per-file analysis, key file selection, and report generation. Token usage is tracked atomically to support concurrent calls.
- **`internal/stats`** — Language detection by extension/filename, producing file and line counts rendered as a Markdown table.
- **`internal/report`** — Pure Markdown writers that take structured analysis data and produce the three output files. No AI logic lives here; it only formats and writes.
- **`internal/tui`** — Two Bubble Tea models: `wizard.go` implements the interactive multi-step configuration wizard that collects source, report type, and output preferences; `pipeline.go` implements the live progress view during analysis, with a gradient progress bar, scrollable results panel, and keyboard navigation.

`cmd/root.go` wires everything together: it runs the wizard (or reads flags directly), calls fetch, analyze, stats, and report in sequence, and drives the TUI pipeline.

## Key Patterns

**Dual AI backend.** The `analyze.Client` transparently switches between SDK and subprocess backends at construction time. This lets the tool work without an API key by shelling out to a locally installed `claude` binary, with no changes to calling code.

**Concurrent analysis with atomic token tracking.** Per-file analysis is designed to run concurrently. Token counts are accumulated with atomic operations rather than a mutex, keeping the hot path lock-free.

**TUI as a message-passing pipeline.** The analysis goroutine communicates with the Bubble Tea UI strictly through `tea.Cmd` messages, following the Elm architecture enforced by Bubble Tea. The `pipeline.go` model handles `FileResultMsg`, resize events, and context cancellation cleanly within this model.

**Pure report writers.** The `report` package has no side effects beyond writing files. Every function accepts structured data and returns a path or error, making it straightforward to test or swap output formats later.

**Role-based file classification.** Files are tagged with a role (e.g., `core`, `entrypoint`, `docs`, `build`) during analysis. The report package uses these roles to group output and the TUI uses them to color-code progress indicators.

## Getting Started

Begin with `main.go` (trivial) then `cmd/root.go`, which is the real entry point and shows the full pipeline flow end-to-end. From there, read the packages in dependency order:

1. `internal/fetch/fetch.go` — understand how repos are acquired and files enumerated.
2. `internal/analyze/analyze.go` — understand the AI client and how prompts are constructed.
3. `internal/stats/stats.go` and `internal/report/report.go` — both are small and self-contained.
4. `internal/tui/wizard.go` then `internal/tui/pipeline.go` — read the wizard first since its `Config` output feeds the pipeline.

Each package has a corresponding `_test.go` file; the tests are good secondary reading to understand expected behavior and edge cases. The `README.md` covers user-facing CLI flags and output formats, which provides helpful context before diving into `cmd/root.go`.

## Repository Statistics

- **Total files:** 20
- **Total lines:** 4166

| Language | Files | Lines | % |
|----------|------:|------:|--:|
| Go | 14 | 3777 | 90.7% |
| Markdown | 1 | 198 | 4.8% |
| JSON | 1 | 13 | 0.3% |

## Documentation Library

- `README.md` — Project overview and getting started guide

## File Tree

```
analyzeRepo/
├── .claude/
│   └── settings.local.json
├── .gitignore
├── LICENSE
├── README.md
├── cmd/
│   ├── root.go
│   └── root_test.go
├── go.mod
├── go.sum
├── internal/
│   ├── analyze/
│   │   ├── analyze.go
│   │   └── analyze_test.go
│   ├── fetch/
│   │   ├── fetch.go
│   │   └── fetch_test.go
│   ├── report/
│   │   ├── report.go
│   │   └── report_test.go
│   ├── stats/
│   │   ├── stats.go
│   │   └── stats_test.go
│   └── tui/
│       ├── pipeline.go
│       ├── pipeline_test.go
│       └── wizard.go
└── main.go
```
