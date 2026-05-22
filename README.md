# GoRefactor

GoRefactor is a command-line harness for **analyzing and refactoring Go code**. Structural edits go through AST-aware commands (not raw text patches), and optional **JSON orchestration plans** use semantic targeting so operations stay valid when line numbers shift.

A companion binary, **`gorefactor-agent`**, drives the same tools with a cheap or local LLM for iterative or autonomous refactors. See [CLAUDE.md](CLAUDE.md) for agent modes, contributor workflow, and the “use gorefactor instead of Write/Edit” rule in this repo.

## Binaries

| Binary | Role |
|--------|------|
| `gorefactor` | Deterministic CLI: analysis, direct edits, lint, plans, undo |
| `gorefactor-agent` | LLM loop that calls `gorefactor`; never edits `.go` files directly |

## Installation

```bash
# Pin to a release (recommended)
go install github.com/chrisophus/gorefactor/cmd/gorefactor@v0.1.0
go install github.com/chrisophus/gorefactor/cmd/gorefactor-agent@v0.1.0

# Or latest from main
go install github.com/chrisophus/gorefactor/cmd/gorefactor@latest
go install github.com/chrisophus/gorefactor/cmd/gorefactor-agent@latest
```

Or use prebuilt binaries from [Releases](https://github.com/chrisophus/gorefactor/releases).

Build from source:

```bash
make build                              # gorefactor (runs test, lint, vet first)
go build -o gorefactor-agent ./cmd/gorefactor-agent
```

## Quick start

```bash
# Structural issues in the module
./gorefactor lint .

# Autofix oversized files where safe
./gorefactor lint . --fix

# Final gate: structural lint + go build + go test
./gorefactor doctor

# One-page summary of a file
./gorefactor inspect path/to/file.go

# Agent: autonomous cleanup from lint findings (needs API key)
./gorefactor-agent -campaign
```

Run `./gorefactor` with no subcommand arguments to print the full command list.

## `gorefactor` commands

### Analysis (read-only)

| Command | Purpose |
|---------|---------|
| `parse` | File structure as JSON |
| `list-functions` | Functions/methods with line counts |
| `recommend` | Ranked extraction candidates |
| `inspect` | Human-readable file summary + lint hints |
| `find-callers` | Who calls a function or `Receiver:Method` |
| `find-uses` | References to a symbol |
| `find-implementations` | Types implementing an interface |
| `find-package-deps` | Package dependency graph |
| `analyze-diff` | Refactoring plan from a patch file |
| `analyze-file-sizes` | Oversized files and split hints |
| `suggest-plan` | Suggested refactoring plan for a file |

### Direct edits (preferred over editing `.go` by hand)

Methods use `Receiver:Method` (no `*` on the receiver). Many commands accept `-` as the last argument to read body content from stdin.

| Command | Purpose |
|---------|---------|
| `create` | New `.go` file |
| `insert` | Code at `at-end`, `after:Func`, `inside:Func`, etc. |
| `replace` | AST statement replace inside a function |
| `replace-text` | Literal text replace inside a function |
| `delete` | Remove a declaration |
| `rename` | Unexported symbol, package-wide |
| `move` | Move declaration to another file |
| `extract` | Extract line range to new function |
| `split` | Auto-split an oversized file |
| `format` | gofmt + goimports in place |

### Automation

| Command | Purpose |
|---------|---------|
| `lint` | Rules: `file-size`, `duplicate-block`, `extract-candidate`, `untested-package`, smells, dead-code; skips `vendor`/`.git`/`node_modules` and `*.gen.go`/`_gen.go`; `--fix` splits oversized files |
| `doctor` | Lint + `go build` + `go test`; non-zero on failure |
| `undo` | Roll back last plan (snapshots under `.gorefactor/`) |

### JSON plans

| Command | Purpose |
|---------|---------|
| `orchestrate` | Run a refactoring plan file |
| `exec` | Single operation from JSON/stdin |
| `generate-templates` | Example plan templates |
| `repl` | Interactive step-by-step refactoring |

Details and JSON plan schema: [ORCHESTRATION_SYSTEM.md](ORCHESTRATION_SYSTEM.md), [orchestrator/README.md](orchestrator/README.md).

## Example: extract method

```bash
gorefactor extract example.go 5 9 calculateSum
```

Given:

```go
func processData(data []int) int {
    sum := 0
    for i := 0; i < len(data); i++ {
        if data[i] > 0 {
            sum += data[i]
        }
    }
    return sum
}
```

The block becomes a new function and the original site calls it (parameters and returns are inferred from the selection).

## `gorefactor-agent` (summary)

Requires `OPENAI_API_KEY` or `ANTHROPIC_API_KEY` (or a local OpenAI-compatible endpoint via `-api-base`).

| Mode | Flag | Use when |
|------|------|----------|
| Agentic (default) | `-spec "..."` | Open-ended refactors; tool loop up to 24 steps |
| Interactive | `-spec "..." -interactive` | Pause after each tool for review/feedback |
| Single-shot | `-single-shot` | One constrained JSON plan (optional `-dry-run`) |
| Campaign | `-campaign` | Fix `gorefactor lint` findings autonomously |

The agent’s `finish` gate runs **`go build` + `go test`** only. For lint + build + test, run **`gorefactor doctor`** yourself or in CI.

Full options and workflows: [CLAUDE.md — Interactive Refactoring with gorefactor-agent](CLAUDE.md#interactive-refactoring-with-gorefactor-agent).

## Project layout

```
cmd/gorefactor/       CLI entrypoint and commands (including extract)
cmd/gorefactor-agent/ LLM harness
parser/               AST → structured JSON
analyzer/             Complexity, diffs, file-size, duplicates; `WalkGoFiles` + `WalkOptions` for lint walks
orchestrator/         JSON plans, semantic targeting, undo snapshots
```

Extraction logic lives in **`cmd/gorefactor/cmd_extract.go`** and orchestrator extract operations—not a separate top-level `extractor/` package.

## Development

```bash
make check              # fmt, vet, lint, test
make test               # tests with race + coverage
./gorefactor doctor     # same gate as CI-style health check
```

Contributor and agent guidance: [CLAUDE.md](CLAUDE.md). Reliability benchmarks: [RELIABILITY.md](RELIABILITY.md).

## License

MIT License
