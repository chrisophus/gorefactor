# CLAUDE.md

Guidance for Claude Code in this repository. Keep this file short: it states the rules and
invariants an agent must know, and points elsewhere for reference material. Do not paste command
references or design docs in here — they drift.

## What this project is

GoRefactor is an **AST-safe edit-and-sense toolkit for Go code, built to be driven by coding
agents**. Two properties define it:

- **Guides**: mutation commands parse before they write, so the failure mode is "command rejects
  the change," never "file silently breaks."
- **Sensors**: analysis commands (`lint`, `find-*`, `skeleton`, `context`, `doctor`) compress repo
  understanding into small structured outputs — orders of magnitude cheaper than reading files.

The deterministic CLI is the product. The JSON plan format (`orchestrate`) and the MCP server
(`gorefactor mcp`) are front-ends over the same engine. `gorefactor-agent` is an experimental LLM
loop around the CLI — see `benchmark/FINDINGS.md` before reaching for it; for scoped tasks the
direct CLI is strictly cheaper.

An honest whole-project assessment, including known weaknesses and the current improvement plan,
is in [docs/project-review-2026-07-19.md](docs/project-review-2026-07-19.md). Read it before
large-scale changes.

## Repo map

| Path | What it is |
|---|---|
| `cmd/gorefactor/` | CLI: command registry (`registry.go`), all commands (`cmd_*.go`), lint rules, MCP server. Commands are thin wrappers over the engines below. |
| `refactor/extract` | Extract-method engine (`PlanMethod`/`Apply`) — importable without `package main`. |
| `refactor/changesig` | Change-signature engine (`Plan`/`Apply`) — importable without `package main`. |
| `internal/astcache` | Parse cache + call-graph index (`ParseCache`, `CallIndexCache`). |
| `internal/goload` | Shared package-loading, AST, and type helpers used by the engines and CLI. |
| `internal/cerr` | Semantic CLI error classification (exit codes, candidates, did-you-mean). |
| `analyzer/` | Complexity/length metrics, lint detectors, diff analysis, symbol/call analysis. |
| `orchestrator/` | The mutation engine: operations, `CodeInserter`, targeting, snapshots/undo, dry-run. |
| `doctor/` | Report/diagnose engine (substrates, fingerprints) behind `doctor --report`. |
| `parser/` | Cheap structural JSON summary of a file (no type checking). |
| `cmd/gorefactor-agent/` | Experimental LLM harness. |
| `benchmark/` | Token-efficiency and agent-reliability measurements; findings in `FINDINGS.md`. |

## Editing .go files: use gorefactor, not Write/Edit

Default rule: modify `.go` files through `./gorefactor` commands — this repo dogfoods its own
harness. Run `./gorefactor` (no args) for the full command list; `./gorefactor help <cmd>` for
usage. The ops you'll use most:

| Want to… | Use |
|---|---|
| Create a file / add a declaration | `create <path> -` / `insert <file> <at-end\|after:F\|inside:F> -` |
| Replace within a function | `edit <file> <Func> <old> <new>` (auto statement-or-text) |
| Replace a whole body | `replace-body <file> <Func> -` |
| Move / delete / rename | `move <src> <F> <dest>` / `delete <file> <F> --safe` / `rename <file> <old> <new>` |
| Batch all-or-nothing | `txn` |
| Understand before changing | `skeleton`, `inspect`, `context <Sym>`, `find-callers`, `find-uses` |
| Roll back | `undo` |

Conventions: methods are addressed as `Receiver:Method`; `-` as the last arg reads stdin; a bare
`--` ends flag parsing when values start with `-`.

`Write`/`Edit` are fine for non-Go files, and as a documented fallback when no command fits.
Note: `rename` is type-aware — it resolves the symbol with `go/types` and rewrites only the
identifiers that share the same object (shadowing locals and same-named fields are left alone),
following it across every file in the package. It requires the package to type-check and is scoped
to **unexported** symbols; use gopls for exported symbols, which may be referenced from packages the
command does not load.

## Build, test, gate

```bash
make build-fast       # compile-only ./cmd/gorefactor (~seconds) — use while iterating
make build            # quality checks + build ./cmd/gorefactor (~minutes)
go build -o gorefactor-agent ./cmd/gorefactor-agent
go test ./...         # full suite (~100s); scope to a package while iterating
make gate             # doctor gate + baseline ratchet — run before committing
```

The lint baseline (`.gorefactor-lint-baseline.json`) is a one-way ratchet: new or worsened
structural findings fail `make gate` and CI. Deliberate growth requires the visible opt-in
(`BASELINE_GROWTH_OK=1` locally, `[baseline-growth]` commit marker in CI). After a cleanup wave,
re-lock with `./gorefactor lint . --write-baseline` and commit the shrunken file.

## Invariants tests will hold you to

- The lint rule registry is pinned by a golden test (`cmd/gorefactor/lint_registry_test.go`) —
  adding/removing a rule means updating it deliberately.
- Doc-drift tests pin constants mentioned in prose; if you change a default, the test tells you
  which doc to fix.
- New capability → new sensor: when adding a mutation/generation capability, add the lint rule that
  detects its misuse, and an autofix only where a single safe transform exists. Every code
  generator gets behavioral tests (a generator once shipped empty test bodies past a green gate).
- Execution results outrank static analysis: the autofix outcome journal
  (`.gorefactor/autofix-outcomes.jsonl`) feeds gate verdicts back into findings. Don't break that
  loop.

## Working agreements

- Commit messages: imperative, scoped prefixes as in `git log` (`lint:`, `doctor:`, `analyzer:`).
- Feature branches; PRs against `main`. CI runs vet, golangci-lint, the structural ratchet, tests.
- Never commit binaries or coverage output.
- Prefer deleting speculative code over documenting around it. If a feature is advertised
  (docs/examples) it must be wired; phantom surface area is a bug (see the review doc).

## Reference docs

- [README.md](README.md) — user-facing overview, install, command survey.
- [ORCHESTRATION_SYSTEM.md](ORCHESTRATION_SYSTEM.md) — JSON plan format.
- [docs/project-review-2026-07-19.md](docs/project-review-2026-07-19.md) — current state, gaps, roadmap.
- [docs/harness-integrity-review-2026-07.md](docs/harness-integrity-review-2026-07.md) — harness lessons.
- [benchmark/FINDINGS.md](benchmark/FINDINGS.md) — measured token economics; consult before using `gorefactor-agent`.
