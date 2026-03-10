# File Summaries

## [entry] Entrypoint

### `cmd/root.go`

Defines the root Cobra CLI command for analyzerepo, handling flag parsing, interactive wizard orchestration, and configuration resolution. Implements the main analysis pipeline coordinating discovery, file selection, per-file analysis, and report generation phases. Serves as the primary entry point that wires together all internal packages.

**Suggestions:**

- [~] **refactor** | file: `cmd/root.go` | effort: small
  Extract the interactive vs non-interactive config resolution logic from `run` into a dedicated `resolveConfig` function to reduce function complexity and improve testability.
  `done_when:` `run` delegates config building to a `resolveConfig(cmd, args) (tui.Config, error)` function and its own body is under 30 lines

- [+] **improvement** | file: `cmd/root.go` | effort: trivial
  The `filepath.Abs` error on line ~147 is silently ignored with `_`. Propagate it: `absOut, err := filepath.Abs(cfg.OutputDir); if err != nil { return err }`.
  `done_when:` `grep -n 'filepath.Abs' cmd/root.go` shows the error is checked and returned

- [+] **improvement** | file: `cmd/root.go` | effort: trivial
  ANSI constants and the `formatTokenCount`/`printPhase`/`printDone`/`printWarn`/`printDim`/`printActive` helpers are defined in this file but are purely presentation utilities. Move them to the `tui` package so `cmd/root.go` stays focused on command wiring.
  `done_when:` All `ansi*` constants and `print*` helpers are absent from `cmd/root.go` and present in `internal/tui/`

- [~] **refactor** | file: `cmd/root.go` | effort: medium
  The `pipeline` function is a single large function handling all five phases. Split it into named phase functions (e.g. `phaseDiscovery`, `phaseAnalysis`, `phaseReports`) called sequentially to improve readability and isolate error handling per phase.
  `done_when:` `pipeline` body is under 40 lines and delegates to at least 3 named phase functions

- [+] **improvement** | file: `cmd/root.go` | effort: trivial
  Package-level flag variables (`flagReports`, `flagFull`, etc.) are mutable globals, making concurrent or multi-command use unsafe. Bind them into a local struct within `init` or use `cmd.Flags().GetString` inside `RunE` instead.
  `done_when:` No package-level `var flag*` declarations exist in `cmd/root.go`

---

### `main.go`

Entry point for the repo-analyze CLI tool that delegates execution to the cmd package. It initializes the application by calling cmd.Execute(), which handles all command routing and logic. The file serves as a minimal bootstrap following standard Go CLI conventions.

**Suggestions:**

- [d] **docs** | file: `main.go` | effort: trivial
  The package comment references 'GitHub repository' but the tool likely supports any git repository or local path — update the doc comment to be accurate.
  `done_when:` grep -q 'local codebase\|any repository' main.go shows a more accurate description without 'GitHub' specificity

---

## [core] Core

### `internal/analyze/analyze.go`

This file implements the Claude AI client and all AI interaction logic for the analyzeRepo tool, including backend detection (API key vs CLI), per-file analysis, key file selection, and report generation. It manages a dual-backend Client that prefers the Anthropic SDK when an API key is present and falls back to the Claude CLI subprocess. Token usage is tracked atomically across concurrent calls.

**Suggestions:**

- [!] **security** | file: `internal/analyze/analyze.go` | effort: trivial
  The isAuthError function matches on the substring '401', which could produce false positives for unrelated error messages (e.g., a URL containing '401' or a business-logic error code). Tighten the match to 'status 401' or check for the specific Anthropic error type instead of a bare numeric substring.
  `done_when:` grep -n '"401"' internal/analyze/analyze.go returns no results, replaced by a more specific sentinel

- [+] **improvement** | file: `internal/analyze/analyze.go` | effort: small
  callCLI silently falls back to treating raw bytes as plain text when JSON unmarshalling fails, discarding any token usage and masking potential CLI errors. Log or surface the parse error so callers and operators can distinguish a real response from a malformed one.
  `done_when:` The JSON fallback branch logs or returns a wrapped error instead of silently continuing

- [~] **refactor** | file: `internal/analyze/analyze.go` | effort: small
  The hardcoded model constant 'claude-opus-4-6' is defined but callAPI independently references anthropic.ModelClaudeOpus4_6, creating two sources of truth. Use the SDK constant exclusively or derive a string from it so a model upgrade only requires one change.
  `done_when:` grep -n 'claude-opus-4-6' internal/analyze/analyze.go returns 0 results (only the SDK enum is used)

- [>] **performance** | file: `internal/analyze/analyze.go` | effort: medium
  maxFileBytes truncates file content at 8,000 characters but does so at a raw byte boundary, which can split multi-byte UTF-8 characters or cut mid-line. Use utf8.ValidString checks or truncate on a line boundary to avoid sending malformed content to the model.
  `done_when:` File truncation logic uses strings.LastIndexByte('\n', ...) or a rune-aware slice to find a safe cut point

- [d] **docs** | file: `internal/analyze/analyze.go` | effort: trivial
  SelectKeyFiles and other exported functions reference a truncated implementation in the snippet, but the package-level doc comment only describes the four high-level concerns without noting the dual-backend fallback strategy or dry-run mode. Expand the package comment to capture these non-obvious behaviors.
  `done_when:` Package comment mentions dry-run mode and the API-key-over-CLI priority fallback

---

### `internal/fetch/fetch.go`

Handles repository acquisition and filesystem traversal for the analyzeRepo tool. Provides functions to clone GitHub repos or resolve local paths, walk the file tree while filtering ignored/binary files, and render a visual directory tree. Acts as the data ingestion layer before analysis begins.

**Suggestions:**

- [!] **security** | file: `internal/fetch/fetch.go` | effort: small
  The `source` argument passed to `exec.CommandContext` as a git URL is not validated beyond prefix matching. A malformed URL containing shell metacharacters won't cause injection (exec avoids the shell), but a URL like `--upload-pack=malicious` could be interpreted as a git flag. Validate that the URL matches a strict pattern (e.g., `regexp.MustCompile(`^https://github\.com/[\w.-]+/[\w.-]+(/.+)?$`)`) before passing it to git clone.
  `done_when:` grep -n 'regexp' internal/fetch/fetch.go returns a compiled regex used to validate the GitHub URL before exec.CommandContext

- [+] **improvement** | file: `internal/fetch/fetch.go` | effort: trivial
  `BuildFileList` silently swallows all walk errors (`return nil` on err). At minimum, log or surface permission errors so callers know the file list may be incomplete.
  `done_when:` grep -n 'err' internal/fetch/fetch.go shows the walk error is either returned or passed to a logger rather than silently discarded

- [>] **performance** | file: `internal/fetch/fetch.go` | effort: small
  `IsBinary` opens and reads every non-ignored file sequentially. For large repos this is the dominant I/O cost. Use `d.Type().IsRegular()` and a worker pool (e.g., `golang.org/x/sync/errgroup` with bounded goroutines) to parallelize reads in `BuildFileList`.
  `done_when:` A benchmark on a repo with 1000+ files shows wall-clock time for BuildFileList is reduced by at least 30% compared to the sequential baseline

- [+] **improvement** | file: `internal/fetch/fetch.go` | effort: trivial
  `ignoreDirs`, `ignoreExtensions`, and `ignoreFilenames` are package-level vars but never modified after init. Declare them as `var` with a comment or use `//nolint` — or better, expose a constructor that accepts additional ignore patterns so callers can extend the defaults without forking the file.
  `done_when:` The ignore sets are either declared as package-level constants/frozen maps with a documented extension point, or a WithIgnorePatterns option is accepted by BuildFileList

- [d] **docs** | file: `internal/fetch/fetch.go` | effort: trivial
  `GetRepo` doc comment does not mention that the returned cleanup function must always be called (even on error paths where it is a no-op) to avoid temp directory leaks. Add a note and consider returning a named type to make the contract explicit.
  `done_when:` The godoc for GetRepo explicitly states 'callers must always invoke cleanup to release any temporary resources'

---

### `internal/report/report.go`

This package handles all Markdown output writing for the repo-analyze tool, producing ANALYSIS.md, ONBOARDING.md, and CLAUDE.md files. It groups file analyses by role, formats suggestions with icons and metadata, and builds a documentation library section from discovered Markdown files. All functions are pure writers that take structured data and return the output path or an error.

**Suggestions:**

- [~] **refactor** | file: `internal/report/report.go` | effort: small
  Extract the suggestion rendering loop in WriteAnalysis into a dedicated renderSuggestion(s analyze.Suggestion) string helper to reduce nesting and improve readability.
  `done_when:` grep -n 'func renderSuggestion' internal/report/report.go returns a match

- [+] **improvement** | file: `internal/report/report.go` | effort: trivial
  The guessMDPurpose fallback map iteration over mdPurpose is non-deterministic; sort keys before iterating to ensure consistent output across runs.
  `done_when:` grep -n 'sort.Strings' internal/report/report.go returns a match inside guessMDPurpose

- [+] **improvement** | file: `internal/report/report.go` | effort: small
  WriteAnalysis, WriteOnboarding, and WriteClaudeMD each call os.WriteFile directly; extract a writeFile(path, content string) error helper to deduplicate error-wrapping logic.
  `done_when:` grep -n 'func writeFile' internal/report/report.go returns a match and all three Write* functions use it

- [d] **docs** | file: `internal/report/report.go` | effort: trivial
  Add a doc comment to guessMDPurpose explaining the three-tiered fallback strategy (exact stem match → substring match → directory heuristic → default).
  `done_when:` grep -B1 'func guessMDPurpose' internal/report/report.go shows a multi-line comment describing fallback tiers

---

### `internal/stats/stats.go`

Computes language statistics for a repository's file list by detecting languages via file extension or filename, then counting files and lines per language. Provides a Compute function that reads file contents and aggregates totals, and a FormatMarkdown function that renders the results as a sorted Markdown table.

**Suggestions:**

- [>] **performance** | file: `internal/stats/stats.go` | effort: small
  Avoid converting the entire file content to string just to count newlines. Use bytes.Count(content, []byte{'
'}) instead of strings.Count(string(content), "\n") to prevent an unnecessary allocation for large files.
  `done_when:` grep -n 'bytes.Count' internal/stats/stats.go returns a match replacing the current strings.Count call

- [+] **improvement** | file: `internal/stats/stats.go` | effort: small
  The root parameter in Compute is accepted but never used. Either remove it from the signature or use it to validate that files are within the root, to avoid a misleading API.
  `done_when:` grep -n 'root' internal/stats/stats.go shows root is either used or removed from the function signature

- [+] **improvement** | file: `internal/stats/stats.go` | effort: trivial
  Binary files (images, compiled artifacts, etc.) will have their lines counted incorrectly since the logic counts newline bytes regardless of content type. Add a simple binary detection heuristic (e.g., check for null bytes in the first 512 bytes) and skip line counting for binary files.
  `done_when:` A file containing null bytes is passed to Compute and its line count is reported as 0 or skipped

---

### `internal/tui/pipeline.go`

Implements the Bubble Tea TUI model for live file-analysis progress, including a gradient progress bar, scrollable panel viewport, and message-passing pipeline between the analysis goroutine and the UI. Handles keyboard navigation, window resizing, and graceful cancellation via context. Renders per-file result panels with role-colored indicators as analysis completes.

**Suggestions:**

- [~] **refactor** | file: `internal/tui/pipeline.go` | effort: small
  Extract gradient rendering (interpolateGradient + renderGradientBar) into a separate file (e.g., internal/tui/gradient.go) to keep pipeline.go focused on the Bubble Tea model lifecycle.
  `done_when:` pipeline.go no longer defines interpolateGradient or renderGradientBar; both functions exist in a sibling tui package file.

- [>] **performance** | file: `internal/tui/pipeline.go` | effort: small
  allPanelLines() re-splits every panel on every View() call. Cache the flat line slice and invalidate it only when m.panels changes (on fileAnalyzedMsg) to avoid O(n) string splitting per frame.
  `done_when:` allPanelLines is replaced by a cached []string field updated only in the fileAnalyzedMsg branch of Update.

- [+] **improvement** | file: `internal/tui/pipeline.go` | effort: trivial
  The easing step threshold (0.002) and tick interval (33ms) are magic numbers. Define them as named constants at the top of the file for clarity and easier tuning.
  `done_when:` grep -n '0\.002\|33\*time' internal/tui/pipeline.go returns no matches; named constants are used instead.

- [+] **improvement** | file: `internal/tui/pipeline.go` | effort: small
  When analysis fails, the failed count increments but there is no visual distinction between failed and succeeded panels in the viewport. Add a visible error indicator (e.g., red border or prefix) to renderFilePanel when err != nil.
  `done_when:` A failed file panel renders with a visually distinct error style; grep for err != nil in renderFilePanel confirms the branch exists.

- [d] **docs** | file: `internal/tui/pipeline.go` | effort: trivial
  pipelineModel fields scrollOffset, panels, and progTicking lack doc comments. Add brief inline comments so the struct is self-documenting without reading Update logic.
  `done_when:` All exported and unexported fields of pipelineModel have an inline comment.

---

### `internal/tui/wizard.go`

Implements the interactive multi-step configuration wizard using Bubble Tea (charmbracelet/bubbletea). Collects user input for source repository, report types, output options, and renders a gradient ASCII banner. Returns a Config struct used downstream by the analysis pipeline.

**Suggestions:**

- [+] **improvement** | file: `internal/tui/wizard.go` | effort: trivial
  termWidth() queries stdout fd (1) for terminal size, but the wizard runs in an alt-screen which uses stderr. Use term.GetSize(2) or rely on the WindowSizeMsg Bubble Tea already delivers, to avoid returning the 80-char fallback when stdout is redirected.
  `done_when:` grep -n 'GetSize' internal/tui/wizard.go shows fd 2 (or the call is removed in favour of the model's width field)

- [~] **refactor** | file: `internal/tui/wizard.go` | effort: small
  newWizardModel duplicates focus-setup logic: it calls si.Focus() outside the switch and then potentially re-assigns m.sourceInput inside the switch. Consolidate into a single location to avoid confusion about which copy of the struct is mutated.
  `done_when:` There is exactly one call to si.Focus() / sourceInput focus setup in newWizardModel

- [>] **performance** | file: `internal/tui/wizard.go` | effort: small
  renderGradientBanner allocates a new lipgloss.Style and calls Render per border cell, which can be dozens of allocations per frame. Pre-compute the border colour strings into a slice outside the render loop.
  `done_when:` lipgloss.NewStyle() inside the borderColor/cell closures is replaced with pre-computed colour strings

- [d] **docs** | file: `internal/tui/wizard.go` | effort: trivial
  The package-level comment references both Wizard and Pipeline components, but Pipeline is implemented in a separate file. Move the package doc to a dedicated doc.go so it isn't tied to wizard.go's lifecycle.
  `done_when:` internal/tui/doc.go exists with the package comment and wizard.go has no package-level comment block

---

## [build] Build

### `go.mod`

Defines the Go module for the analyzeRepo project, specifying the module path as github.com/rlnorthcutt/analyzeRepo and requiring Go 1.26. It declares all direct and transitive dependencies including the Anthropic SDK, Charm TUI libraries (bubbletea, bubbles, lipgloss), Cobra CLI framework, and various utility packages.

**Suggestions:**

- [+] **improvement** | file: `go.mod` | effort: small
  Separate direct dependencies from indirect ones using two distinct require blocks, and mark only true transitive deps as `// indirect`. Currently all deps are marked indirect, which obscures which packages the module directly imports.
  `done_when:` Running `go mod tidy` produces no changes and direct imports lack the `// indirect` comment

- [+] **improvement** | file: `go.mod` | effort: trivial
  Go 1.26 does not exist yet (current stable is 1.23/1.24). Verify and correct the `go` directive to match the actual minimum required Go version to avoid toolchain confusion.
  `done_when:` `go version` in the directive matches a released Go version (e.g., `go 1.23`)

---

## [docs] Docs

### `README.md`

The README provides a comprehensive overview of the analyzerepo tool, which analyzes GitHub repositories or local codebases using Claude AI to generate onboarding guides, per-file analysis with improvement suggestions, and CLAUDE.md context files. It covers installation, CLI usage, how the tool works, and output formats. It serves as the primary user-facing documentation for the project.

**Suggestions:**

- [d] **docs** | file: `README.md` | effort: trivial
  The --reports flag lists 'improvement' as a valid value in the CLI reference table, but the flag description text above it says 'onboarding, improvement, claude, all'. Ensure these are consistent and match the actual flag values accepted by the binary.
  `done_when:` grep -n 'improvement\|analysis' README.md shows consistent naming between the flag description, the report types table, and the output filename (ANALYSIS.md)

- [d] **docs** | file: `README.md` | effort: trivial
  The ANALYSIS.md role classification list in the 'What you get' section omits 'data' and 'other' roles that appear in the actual analysis schema, making the docs incomplete.
  `done_when:` grep -A5 'role classification' README.md includes all valid role values matching the schema used in analysis prompts

- [+] **improvement** | file: `README.md` | effort: small
  Add a troubleshooting or FAQ section covering common failure modes: rate limiting, large repos exceeding token estimates, missing API key errors, and repos with no analyzable files.
  `done_when:` README.md contains a Troubleshooting or FAQ section with at least 3 common error scenarios and their resolutions

---

