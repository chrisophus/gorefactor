# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.13.0] - 2026-07-20

### Added
- **Focused lint policy config** — `tracked_artifact` allowlists, `lint.exclude_test_files`,
  `lint.exclude_packages`, `lint.thresholds.high-coupling`, and `baseline.enabled/file` in
  `.gorefactor.yaml`; `--no-baseline` CLI override.
- **Lint policy filter** — excluded findings are removed before severity remapping and baseline
  comparison.
- **Literal excluded-path liveness** — `orphaned-config-path` recognizes existing directories
  such as `node_modules` without recursively walking them.

### Changed
- **Tier contract documented** — error = CI gate, warning = baselined debt, info = opt-in
  (`README.md`, `config/tier.go`).

## [0.12.0] - 2026-07-20

### Added
- **Importable refactor engines** — `refactor/extract`, `refactor/changesig`, and
  `internal/astcache` (plus shared `internal/goload`, `internal/cerr`); CLI
  commands are thin wrappers so MCP and library consumers need not import
  `package main`.
- **Types-aware `rename`** — rewrites identifiers by `go/types` object identity
  across package files; shadowing locals and same-named fields are left alone.
- **New lint sensors** — `test-only-live`, `advertised-but-unwired`, `god-package`,
  and (from the P0 harness wave) `tracked-artifact` plus strengthened
  `stranded-comment` / config-liveness checks.
- **Command I/O metadata** — `ReadOnly` / `Mutates` / `Idempotent` / `MCPTool` /
  `TxnSafe` on `Command`; MCP and `txn` allowlists are derived from metadata
  instead of hand-synced slices.
- **`make gate-self-clean`** — aspirational release bar for an empty lint
  baseline (sunset **2026-10-19**); default `make gate` keeps the one-way ratchet.
- **Doctor tool auto-provision** — bootstraps golangci-lint (into
  `.gorefactor/tools/`), and uses `go tool` for deadcode/govulncheck when declared
  in `go.mod` (`GOREFACTOR_NO_TOOL_BOOTSTRAP` to opt out).
- **Campaign cost-of-pass** — junior Haiku vs frontier Sonnet matrix published in
  `benchmark/FINDINGS.md` (keep campaign/agentic as headline agent surface).

### Changed
- **Doctor gate** — structural stage runs the full lint registry (same family as
  `doctor --report`), not the legacy 3-check subset.
- **One mutation path** — journal is the sole undo system; plan snapshots,
  `SkipSnapshot`, and package-global `activeTxn` are removed; `orchestrator.Batch`
  folds `txn` into the journal.
- **`gorefactor-agent` campaign-or-cut** — `-single-shot` and `-interactive`
  removed; triage, default agentic tool-calling, and `-campaign` remain.
- **Go toolchain** — `go 1.26.5`.
- **Doctor score** — size-normalized proxy tier, partial credit for threshold
  proxies, data-clumps carrier false-positive fix.
- **Lint diet** — keep autofix-backed gorefactor rules (`funcorder-*`,
  `error-not-wrapped`, `complexity` / `long-function` with extract paths);
  disable overlapping golangci `cyclop` / `funlen` / `dupl`.
- **Phantom targeting retired** — unused `TargetSpecification` fields and example
  plan references removed.

### Fixed
- **`doctor` temporal substrate** — do not wrap a nil workflowcheck error on
  success.
- **`undo` MCP hint** — marked non-idempotent so retries cannot pop multiple
  journal entries.
- **Journal concurrency** — serialize journal/batch I/O for concurrent MCP tool
  calls.
- **CI toolresolve tests** — isolate PATH so module-local golangci cache is
  exercised when CI installs golangci-lint globally.
- **`--fail-on warning`** — info-tier findings no longer fail the gate.

## [0.11.1] - 2026-07-17

### Fixed
- **`complexity` and `long-function` extraction autofix disabled** — the
  automated extraction engine produced unreliable output in some cases (name
  collisions, invalid function signatures, missing returns), causing build
  failures after `lint --fix`. Both autofixes are disabled until the extractor
  is hardened; `recommend --reduce-complexity --apply` and
  `recommend --reduce-length --apply` are unaffected.

### Changed
- Applied safe auto-fixes repo-wide: `error-not-wrapped` and `funcorder`
  issues resolved across `analyzer/`, `doctor/`, and test files.

## [0.11.0] - 2026-07-17

### Fixed
- **`wrap-errors`: sentinel branch before bare `return err` was skipped** —
  `if err != nil` blocks containing an `errors.Is` sentinel branch (returning
  a translated error with `nil` in the error slot) followed by a bare
  `return nil, err` were skipped entirely. `findBareErrReturn` now tolerates
  leading sentinel-only branches and wraps the final bare return.
- **`wrap-errors`: bare `return err` inside loops was skipped** — `if err !=
  nil` blocks nested inside `for`/`range`/`switch`/`select` bodies were never
  visited. The processor now recurses into all compound statement types.
- **`wrap-errors`: doc comment of next function embedded in `fmt.Errorf`** —
  When the last statement in a function was `return nil, err`, the go/printer
  could pull the doc comment of the immediately following function into the
  `fmt.Errorf(...)` argument list and remove it from its correct position.
  Fixed by propagating the original `err` identifier's source position to the
  replacement node so the printer has a precise anchor.
- **`error-not-wrapped` lint rule: false positives inside function literals** —
  `return err` inside `filepath.Walk`/`WalkDir` callbacks (and any closure)
  was reported as an unwrapped error on the outer exported function. The
  detector now stops `ast.Inspect` from descending into `*ast.FuncLit` nodes.

## [0.10.4] - 2026-07-16

### Fixed
- **`doctor --config PATH`**: `doctor --fix` now accepts `--config` and passes
  it through to both the lint stage and the autofix pass. Previously `doctor`
  ignored the `walk:` skip rules in `.gorefactor.yaml` (no `--config` flag
  existed), causing it to modify generated files such as sqlc output that
  `lint --fix --config` correctly excluded. Both `doctorLintStage` and
  `doctorAutoFix` now build their walk options from the loaded config rather
  than `analyzer.DefaultWalkOptions()`.

## [0.10.3] - 2026-07-16

### Fixed
- **`--version` on GoReleaser binaries**: v0.10.2 reported `(devel)` because
  `-buildvcs=false` suppressed both the dirty suffix and the version stamp.
  GoReleaser now injects the tag via `-ldflags -X version.injected`, which
  takes priority over `debug.ReadBuildInfo()`. `go install @vX.Y.Z` still
  falls through to build info; local `go build` still reports `(devel)`.

## [0.10.2] - 2026-07-16

### Fixed
- **`+dirty` version suffix**: GoReleaser binaries no longer report `v0.10.x+dirty`.
  Root cause was the Go toolchain stamping the binary with VCS state when the
  build directory was unclean. Fixed by passing `-buildvcs=false` to suppress
  the stamp in release builds.

## [0.10.1] - 2026-07-16

### Changed
- **Version reporting**: `--version` now reads the version directly from the Go
  module's build info (`runtime/debug.ReadBuildInfo`) instead of a hand-maintained
  constant. GoReleaser and `go install @vX.Y.Z` binaries always self-report the
  correct tag with no manual sync step. Local `go build` builds report `(devel)`.

## [0.10.0] - 2026-07-16

### Added
- **`extract-var` / `extract-const`**: new mutation guides that bind an
  expression inside a function to a named local (`name := expr` /
  `const name = expr`) and rewrite the occurrence. The binding is placed in the
  same block as the occurrence — descending into nested `if`/`for`/`switch`
  bodies — so evaluation timing and scope are preserved and single-occurrence
  extraction is always behavior-preserving. `--all` rewrites every occurrence;
  `extract-const` statically rejects non-constant expressions (calls, indexing,
  composite/func literals, address-of, and local-variable/parameter operands).
- **`lint --baseline` / `--write-baseline`**: ratchet mode. Record current
  findings to a committable `.gorefactor-lint-baseline.json`, then fail only on
  new-or-worsened issues. Matching is line-number-independent (fingerprint =
  file + rule + digit-normalized message), so a finding that merely shifts when
  unrelated code is added stays suppressed. `--baseline-file PATH` overrides the
  path. Enables adopting the linter on an existing backlog without paying it
  all down first.

### Changed
- **CI** now builds `gorefactor` and runs its own structural lint gate
  (`lint . --fail-on error`), so the tool's error-tier rules ratchet on every
  PR rather than only via the optional local pre-commit hook.
- **Agentic runs** now aggregate `.gorefactor/failures.jsonl` into a bounded
  "known failure modes" block appended to the system prompt (top rejected tools
  with a representative reason, plus capability-gap/budget-hit counts), closing
  the mistake-cannot-recur loop. Empty on a cold repo, so a fresh checkout pays
  no tokens.
- A doc-drift test now asserts every registered command appears in the
  `CLAUDE.md` command reference, keeping the hand-maintained table in sync with
  `getCommands()` (this caught `wrap-errors`, which was undocumented).

## [0.9.0] - 2026-07-11

### Added
- **`lint --fix-level aggressive`**: a second autofix tier above the default
  `safe` level. Aggressive fixes are mechanical but not provably
  behavior-preserving, so the flag is only accepted together with
  `--fix --verify` — every aggressive fix is build+test gated and individually
  reverted on failure. It unlocks:
  - **`long-function` autofix**: shortens an over-threshold function by
    extracting its greedily-chosen largest top-level blocks under generated
    names (`gorefactor recommend --reduce-length <file> <Func> --apply`).
  - **`extract-candidate` autofix**: extracts the flagged function's largest
    top-level block, re-derived at apply time so line numbers never go stale.
  - **`complexity` autofix upgrade**: passes `--allow-returns` so complexity
    concentrated in return-bearing error branches stops being skipped.
  - **Non-adjacent log/return fixes**: `remove-log-return --aggressive` (and
    the `if-err-log-return` autofix at the aggressive level) also deletes a
    log separated from the flagged return by other statements, wrapping the
    bare `return err` exactly once even when several logs precede it.
  - **Module-wide dead exported functions**: the `dead-code` rule additionally
    flags exported top-level functions referenced nowhere in the module
    (`analyzer.DetectDeadExportedFunctions`). Methods are excluded —
    reflection/interface dispatch can invoke them with no in-module ident —
    and out-of-module consumers are exactly why this is verify-gated.
- **`extract --allow-returns`**: the extract engine can now lift a
  return-bearing block into a `(results..., done bool)` helper; each direct
  `return e1, e2` becomes `return e1, e2, true`, the helper falls through to a
  naked `return` (zero values, `done=false`), and the call site propagates via
  `if r0, r1, done := helper(...); done { return r0, r1 }`. Blocks that also
  assign outer variables used later, naked returns under named results, and
  single-call multi-value returns are refused with teaching errors. Returns
  inside function literals are no longer treated as barriers at all.
- **`recommend --reduce-length <file> <Func|Recv:Method> [--max-lines N]
  [--apply [--allow-returns]]`**: line-count analog of `--reduce-complexity`;
  backs the two new autofixes. `--reduce-complexity --apply` also accepts
  `--allow-returns` now.

### Fixed
- **Extraction write-back for mutated outer variables**: `extract` treated an
  outer variable assigned inside the block as a by-value parameter, silently
  discarding the mutation (`total += ...` extracted into a helper changed
  program output). Such variables are now returned and written back at the
  call site (`total = helper(items, total)`, `=` not `:=`), and writes that
  reach shared memory (pointer/slice/map paths) are recognized as needing no
  write-back. Return order is now deterministic (declaration order).
- **`long-function` and `deep-nesting` rules were silently dead**:
  `FunctionMetricsForFile` passed a typed-nil `[]byte` to `parser.ParseFile`,
  which treats it as empty source, so every file "failed to parse" and the
  rules never fired.

## [0.8.0] - 2026-07-10

### Added
- **Autofixes for the log-propagation lint family**: `lint --fix` now covers
  seven rules (up from three). New mutation commands carry the transforms:
  - **`remove-log-return <file> [--rule <name>]`** fixes `if-err-log-return`,
    `wrap-log-return`, and `wrap-bridge-log-return` by deleting the redundant
    log statement adjacent to an error-propagating return and wrapping a bare
    `return err` it uncovers. Only adjacent log/return sites get an autofix;
    lint attaches the fix only to issues the fixer can actually resolve.
  - **`wrap-sentinels <file> <Sentinel>`** fixes `duplicate-bare-sentinel` by
    wrapping each bare sentinel return with `fmt.Errorf("<context>: %w", ...)`.
- **`lint --fix --verify`**: each autofix is now self-checking. The affected
  package is snapshotted before the fix, then `go build ./...` + `go test ./...`
  runs (doctor's gate minus lint); if it goes red the fix is reverted and the
  remaining fixes continue. Kept fixes are journaled so `undo` still works;
  reverted fixes leave the tree untouched. This makes bulk `--fix` trustworthy
  for unsupervised cleanup — the over-approximate sensors (e.g. a `dead-code`
  symbol reached only via reflection or build tags) are backstopped by the gate.
  The summary reports `N applied, M reverted (gate failed), K failed to apply`.

### Changed
- **golangci-lint moved from `lint` to `doctor`**: `gorefactor lint` no longer
  shells out to golangci-lint (26 default rules, all in-process); `doctor` now
  runs it as its own gate stage, skipped cleanly when the binary or a
  `.golangci.*` config is absent. Previously it was backwards: `lint` ran it
  and `doctor` didn't.

### Fixed
- **`replace`/`edit`/`remove` statement matching no longer clobbers enclosing
  code**: the matcher required exact (whitespace-normalized) statement equality
  and recurses into nested blocks. Previously a pattern that was only a
  fragment of a statement — or a statement nested inside a loop — matched its
  entire enclosing top-level statement by substring, and `replace` silently
  replaced all of it. Fragments now fall back to text replace under `edit` and
  report not-found under `replace`; nested statements are replaced in place.
- **`insert before:/after:/inside:` accepts `Receiver:Method` locators**,
  matching every other command; previously the receiver form failed with
  not-found while listing the same name as a candidate.
- **`doctor <dir>` runs build/test in the target directory**: the `go build` /
  `go test` stages never set the working directory, so `doctor some/dir` could
  lint one module and build/test another.
- **`set-doc` accepts already-formatted comment blocks**: `//` markers are
  stripped before reflowing and blank comment lines survive as paragraph
  breaks. Previously the markers were treated as words, producing run-on
  comments with embedded `//` mid-sentence.

## [0.7.0] - 2026-07-06

### Added
- **`recommend --reduce-complexity <Func> [--threshold N]`**: threshold-driven
  mode that greedily picks the minimum set of top-level blocks to extract to
  bring an over-threshold function below `--threshold` (default 15), instead of
  surfacing micro-blocks.
- **`lint --info` / `--verbose`**: `[info]` issues (e.g. `high-blast-radius`,
  `untested-*`) are now hidden by default so actionable warnings aren't buried.
  `--info` shows them (collapsing per-file `high-blast-radius` into one summary
  line); `--verbose` shows everything uncollapsed.
- **`lint.duplicate-ignore`** config key in `.gorefactor.yaml`: extra normalized-
  code patterns excluded from `duplicate-block` detection (additive).
- **`format` exposed as an MCP write tool** under `--allow-write`.

### Changed
- **`extract` errors now name the nearest extractable range** instead of the
  opaque "no complete statements in lines X-Y".
- **`extract` warns on suspiciously small results** (fewer than 2 statements, or
  more than 40% smaller than the requested range) after silently trimming to
  statement boundaries.
- **`extract` explains control-flow barriers**: `continue`/`break`/`goto`/
  `fallthrough` that target an enclosing scope are named, with a suggested
  early-return restructuring.
- **`duplicate-block` false positives reduced**: minimum block size raised to 3
  statements, and canonical error idioms (`if err != nil { return err }`, etc.)
  are excluded by a built-in normalized-form deny-list.

## [0.4.0] - 2026-06-07

### Changed
- **`gorefactor lint` is ~13× faster** (~9.1s → ~0.7s on this repo), with
  byte-identical output. Rules now run concurrently (`errgroup`) with a
  deterministic final sort.

### Fixed
- **dead-code rule O(functions × files) blowup** (~7.3s → ~0.2s):
  `DetectDeadFunctions` rebuilt the entire call graph for every unexported
  function, and snippet extraction re-read each file from disk per call
  expression. `UseAnalyzer.Parse` is now idempotent, file lines used for
  snippets are cached, and the call graph is reset per build.
- **premature-abstraction** now issues a single `packages.Load` over the
  explicit directory set instead of one toolchain invocation per directory
  (~0.69s → ~0.12s), with the scanned set unchanged.

### Added
- Hidden `--cpuprofile` and `--profile-rules` flags on `lint`, plus a
  `BenchmarkLint` benchmark, for performance work.

## [0.3.1] - 2026-06-07

### Added
- `--quiet` mode for the `lint` command.

## [0.3.0] - 2026-06-01

### Added
- YAML configuration support.
- Rule tiers (configurable severity/enablement per rule).
- Generic walk options for file traversal.

## [0.2.0] - 2026-06-01

### Added
- Additional `lint` flags.
- Log-propagation rules.
- Marketplace walk preset.

## [0.1.0] - 2026-05-19

### Added
- Initial release: JSON-based orchestration, semantic targeting, method
  extraction, and the structural linter (file-size, duplicate-block,
  extract-candidate, smell rules, complexity, dead-code, and more).

[0.4.0]: https://github.com/chrisophus/gorefactor/compare/v0.3.0...v0.4.0
[0.3.1]: https://github.com/chrisophus/gorefactor/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/chrisophus/gorefactor/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/chrisophus/gorefactor/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/chrisophus/gorefactor/releases/tag/v0.1.0
