# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
