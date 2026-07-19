# GoRefactor

GoRefactor is a deterministic, AST-aware command-line tool for analyzing and refactoring Go code. It's designed for **safe, repeatable structural edits** that won't silently break your code.

**Key innovations:**
- **Semantic targeting**: Refactoring operations use function names, code patterns, and variable analysis instead of fragile line numbers—your plans stay valid even when code evolves
- **Harness pattern**: Operations refuse to produce malformed Go (AST-aware, with goimports built-in), so the failure mode is “command rejects” not “file silently breaks”
- **LLM-integrated**: Companion binary `gorefactor-agent` enables iterative or autonomous refactors with Claude, GPT, or local LLMs—the LLM proposes operations, the tool executes them deterministically
- **Batch refactoring**: JSON orchestration plans let you specify 10 similar edits once and apply them everywhere

See [CLAUDE.md](CLAUDE.md) for agent modes, developer workflow, and architectural patterns.

## Binaries

| Binary | Role |
|--------|------|
| `gorefactor` | Deterministic CLI: analysis, direct edits, lint, plans, undo |
| `gorefactor-agent` | LLM loop that calls `gorefactor`; never edits `.go` files directly |

## Installation

```bash
# Pin to a release (recommended)
go install github.com/chrisophus/gorefactor/cmd/gorefactor@v0.6.0
go install github.com/chrisophus/gorefactor/cmd/gorefactor-agent@v0.6.0

# Or latest from main
go install github.com/chrisophus/gorefactor/cmd/gorefactor@latest
go install github.com/chrisophus/gorefactor/cmd/gorefactor-agent@latest
```

Or use prebuilt binaries from [Releases](https://github.com/chrisophus/gorefactor/releases).

## Why GoRefactor? (The value proposition)

### 1. **Safe-by-design**
- Operations **refuse to produce malformed code** (AST-aware, runs goimports, validates before writing)
- Failure mode: "command rejects the change" not "silently broken file"
- Compare to: `sed` scripts (can silently break) or manual edits (typos, missed imports)

### 2. **Resilient to code changes**
Refactoring plans use **semantic targeting**, not line numbers:
```json
{
  "type": "extract_method",
  "target": {
    "functionName": "ProcessPayment",
    "variableNames": ["card", "amount"],
    "codePattern": "if.*checksum"
  }
}
```
Even if someone adds 10 lines above the extraction block, the plan still works. With line numbers, it would be off by 10.

### 3. **LLM-friendly**
The agent (`gorefactor-agent`) **never directly edits `.go` files**—it proposes operations and calls `gorefactor` deterministically. This means:
- **No prompt injection**: LLM can't accidentally write malformed code
- **Cheap**: Uses local or cheap LLMs (qwen 14b: 80% success rate on complex refactors)
- **Auditable**: Every change is a gorefactor command, not opaque LLM-generated code
- **Recoverable**: Built-in `undo` rolls back snapshots

### 4. **Batch operations**
Apply the same refactoring to 10 files in one operation:
```bash
# One JSON plan, all files
gorefactor orchestrate consolidate-error-handling.json
# vs. manual: repeat the same change 10 times, pray for consistency
```

### 5. **Built-in linting with autofix**
```bash
gorefactor lint .                  # Detect structural issues
gorefactor lint . --fix            # Auto-fix what's safe (split files, remove dead code, wrap errors, drop redundant logs)
gorefactor lint . --fix --verify   # ...and revert any fix that breaks `go build`/`go test`
gorefactor lint . --fix --verify --fix-level aggressive  # also shorten long functions, lift return-bearing blocks, delete module-wide dead exported funcs, fix non-adjacent log/return
gorefactor lint . --fail-only      # Show only blocking (error-severity) issues
gorefactor doctor                  # Lint + golangci-lint + build + test (final gate)
```

The default rule set has 41 rules, grouped by concern (canonical list in `cmd/gorefactor/lint_registry_test.go`):

- **Size & structure**: `file-size` (>500 lines, split hints by receiver/prefix), `long-function`, `deep-nesting`, `complexity` (cyclomatic), `extract-candidate`
- **Duplication**: `duplicate-block` (>100-line clones with consolidation hints), `duplicate-bare-sentinel`
- **Design smells**: `god-object`, `large-class`, `fat-interface`, `excessive-params`, `excessive-returns`, `data-clumps`, `type-switch`, `premature-abstraction`, `high-coupling`
- **Error handling**: `error-not-wrapped` (bare `return err`), `if-err-log-return`, `wrap-log-return`, `wrap-bridge-log-return`
- **Ordering**: `funcorder-constructor` (constructor must follow the struct, before its methods), `funcorder-struct-method` (exported methods before unexported), `funcorder-function` (exported top-level functions before unexported ones, excluding constructors and `init()`) — ports golangci-lint's `funcorder` default checks plus its opt-in `function` check
- **Coverage**: `untested-function`, `untested-package`
- **Test hygiene**: `vacuous-test`, `sleep-in-test`
- **Libraries / lifecycle / concurrency**: `fatal-in-library`, `unstopped-ticker`, `naked-goroutine`, `pass-through-param`
- **Performance**: `regexp-compile-in-func`, `string-concat-in-loop`, `linear-search-in-loop`
- **Dead code**: `dead-code` (unused funcs/types across the module)
- **Impact / self-audit**: `high-blast-radius`, `low-gorefactor-adherence`
- **Harness residue**: `generated-name`, `byvalue-buffer`, `stranded-comment`, `orphaned-config-path`
Both `go-arch-lint` and `golangci-lint` are deliberately kept out of the `lint` rule set — `doctor` runs each as its own stage (both self-skip when the binary or config is absent), keeping `lint` fast and fully in-process. Run them independently with `go-arch-lint check` / `golangci-lint run`, or together via `gorefactor doctor`.

`--fix` autofixes the rules with a single safe transformation: `file-size` (via `split`), `dead-code` (delete unreferenced decls), `error-not-wrapped` (wrap with `fmt.Errorf(... %w)`), the log-propagation rules (via `remove-log-return` — delete the redundant log next to a propagating return, wrap a bare `return err`), `duplicate-bare-sentinel` (via `wrap-sentinels`), and `funcorder-constructor`/`funcorder-struct-method`/`funcorder-function` (via `reorder-funcorder`). Add `--verify` to make each fix self-checking: fixes are applied in batches of up to 8 (`defaultAutoFixBatchSize`), each batch is gated by `go build ./...` + `go test ./...`, and a failing batch is bisected so only the offending fix is reverted while the rest are kept — so bulk `--fix` is safe to run unsupervised even where a sensor over-approximates.

**vs. alternatives:**
- **gopls**: Great for interactive refactoring in an IDE, slow for CLI (60× slower cold-start)
- **go/analysis**: Low-level; you write the analysis rules yourself
- **golangci-lint**: Linting only; no refactoring or suggestions
- **Manual scripts**: Fragile (line numbers, imports), easy to break

### 6. **Iteration and feedback**
The agent supports interactive mode:
```bash
gorefactor-agent -spec "extract validation logic" -interactive
# After each tool call, you see the changes and can provide feedback
# Step 1: find_uses PaymentValidator
#   → Found 3 callers. Continue? [c/f/r/s/a/?]
```

This bridges the gap between fully manual and fully autonomous refactoring.

Build from source:

```bash
make build                              # gorefactor (runs test, lint, vet first)
go build -o gorefactor-agent ./cmd/gorefactor-agent
```

## When to use GoRefactor

| Scenario | GoRefactor | Manual edit | gopls/IDE |
|----------|-----------|------------|-----------|
| Extract a 20-line method | ✅ Auto-infer params/returns | Need to count locals | Works but slower |
| Rename across 5 files | ✅ Semantic (handles shadowing) | Error-prone | Works, but requires IDE |
| Move function to new file | ✅ Auto-handle imports | Manual import edits | Works |
| Split a 500-line file | ✅ Auto-suggest by complexity/receiver | Not practical | N/A |
| Find dead code | ✅ Cross-file analysis | Guess/search | Limited |
| Fix 20 files (same pattern) | ✅ JSON plan, one execution | Repeat 20 times | Not applicable |
| Refactor driven by LLM | ✅ Agent calls gorefactor | Agent must edit directly | Not applicable |
| **Bottom line** | **Safe, repeatable, fast** | **Error-prone** | **Interactive only** |

**Best fit:** Large refactors, automated cleanup in CI, LLM-driven changes, or teams applying consistent patterns across multiple files.

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
| `find-package-deps` | Package dependency graph + circular-import detection |
| `analyze-diff` | Refactoring plan from a patch file |
| `analyze-file-sizes` | Oversized files and split hints |
| `suggest-plan` | Suggested refactoring plan for a file |
| `callgraph` | Transitive call tree (callees, or `--callers`) for a function/method |
| `context` | One-shot LLM context pack for a symbol: def, callers, signature types, tests (`--budget N`) |
| `skeleton` | File with function bodies elided — token-cheap file shape |
| `search-ast` | Structural search; match a statement/expression pattern (`$_` wildcard) |
| `api-diff` | Diff the exported API surface vs a git ref (default HEAD) |
| `review` | Structural quality review of changed functions vs a git ref |
| `test-affected` | Map changed files to transitively affected packages and their tests (`--run`) |
| `architect` | Generate a starter `go-arch-lint.yml` from the import graph |
| `history` | List the journal of mutation operations |

### Direct edits (preferred over editing `.go` by hand)

Methods use `Receiver:Method` (no `*` on the receiver). Many commands accept `-` as the last argument to read body content from stdin.

| Command | Purpose |
|---------|---------|
| `create` | New `.go` file |
| `insert` | Code at `at-end`, `after:Func`, `inside:Func`, etc. |
| `replace` | AST statement replace inside a function |
| `replace-text` | Literal text replace inside a function |
| `replace-body` | Replace a function/method body wholesale |
| `delete` | Remove a declaration (`--safe` checks callers first) |
| `rename` | Unexported symbol, package-wide |
| `move` | Move declaration to another file |
| `extract` | Extract line range to new function |
| `extract-var` | Bind an expression to a new local variable and rewrite occurrence(s) |
| `extract-const` | Like `extract-var` but emits a local `const` |
| `inline` | Inline a simple function into call sites and delete it |
| `add-field` | Add a struct field; optionally rewrite positional literals to keyed |
| `change-signature` | Add/remove/rename a parameter and update all call sites |
| `change-receiver` | Switch a method receiver between value and pointer form |
| `set-doc` | Set/replace the doc comment on a declaration |
| `insert-switch-case` | Add a `case` to the switch inside a function |
| `insert-map-entry` | Append an element to a map/slice composite literal |
| `replace-in-literal` | Replace text inside one string literal (AST-scoped; use `--` for dash-leading args) |
| `add-test` | Generate a table-driven test scaffold for a function/method |
| `extract-interface` | Generate an interface from a type's exported method set |
| `implement-interface` | Generate compiling method stubs for unimplemented interface methods |
| `wrap-errors` | Rewrite bare `return err` in `if err != nil` blocks to `fmt.Errorf` wrapping |
| `hoist-regexp` | Hoist function-local constant `regexp.MustCompile` calls to package level |
| `split` | Auto-split an oversized file |
| `format` | gofmt + goimports in place |

### Automation

| Command | Purpose |
|---------|---------|
| `lint` | 41 structural rules (size, duplication, smells, error handling, ordering, coverage, dead-code, arch); skips `vendor`/`.git`/`node_modules` and `*.gen.go`/`_gen.go`. `--fix` autofixes `file-size`, `dead-code`, `error-not-wrapped`, `complexity`, the log-propagation family, `funcorder-constructor`/`funcorder-struct-method`/`funcorder-function` (via `reorder-funcorder`) (add `--verify` to revert any fix that breaks build/test). `--fix-level aggressive` (requires `--fix --verify`) additionally autofixes `long-function`/`extract-candidate` by extraction, lifts return-bearing blocks, fixes non-adjacent log/return pairs, and deletes module-wide unreferenced exported functions. `--fail-only` shows blocking issues only |
| `doctor` | Lint + `go build` + `go test`; non-zero on failure. `--report` merges all substrates into one advisory report |
| `adherence` | Harness self-audit: fraction of changed `.go` files edited via gorefactor vs raw Write/Edit |
| `intent` | Declare a deliberate exported-API change so the apidiff gate passes it |
| `txn` | Apply a batch of mutation commands transactionally (all-or-nothing, single undo unit) |
| `undo` | Undo the most recent journaled mutation (or restore a named plan snapshot) |
| `init-agent-rules` | Write the agent-rules snippet into `CLAUDE.md` / `.cursorrules` / `AGENTS.md`; `--mcp` also emits a `.mcp.json` pointing a client at `gorefactor mcp` |
| `mcp` | Run a stdio MCP server exposing gorefactor's tools to any MCP client (see below) |

### JSON plans

| Command | Purpose |
|---------|---------|
| `orchestrate` | Run a refactoring plan file |
| `exec` | Single operation from JSON/stdin |
| `generate-templates` | Example plan templates |
| `repl` | Interactive step-by-step refactoring |

Details and JSON plan schema: [ORCHESTRATION_SYSTEM.md](ORCHESTRATION_SYSTEM.md), [orchestrator/README.md](orchestrator/README.md).

### MCP server

`gorefactor mcp` runs a stdio [Model Context Protocol](https://modelcontextprotocol.io)
server (built on the official Go SDK) so any MCP client — Claude Code, Cursor,
Copilot — gets gorefactor's exact, AST/type-based Go intelligence as native
tools. Unlike embedding-based indexers it's exact for Go *and* it can edit.

```bash
# Point the current project's client config at the server
gorefactor init-agent-rules --mcp        # writes/merges .mcp.json

# Read-only by default: parse, skeleton, callgraph, blast-radius,
# find-callers/uses, search-ast, lint, ... plus skeleton/inspect/context
# served as MCP resources (gorefactor://skeleton/<path>, etc.)
gorefactor mcp

# Opt in to the safe-edit guides (create/insert/replace/move/rename/delete/txn/undo)
# as destructive-annotated tools. Requires a clean git worktree so every edit is
# reversible with `git reset --hard` (pass --allow-dirty to skip that check).
gorefactor mcp --allow-write
```

Design and phase status: [docs/mcp-server-plan.md](docs/mcp-server-plan.md).

### Deploying to other Go projects

GoRefactor is project-agnostic — install it once, then wire it into any Go
module. The generated config references `gorefactor` on your `PATH` (no hardcoded
paths), so the same setup works everywhere.

```bash
# 1. Install binaries + helper scripts globally (from a clone of this repo)
make install
#    → gorefactor, gorefactor-agent, gorefactor-delegate, gorefactor-init-project
#      installed to $(go env GOPATH)/bin

# 2. In any other Go project, wire in rules + MCP + a commit-time gate
cd ~/my-other-project
gorefactor-init-project            # read-only MCP (analysis tools; edits via CLI/rules)
gorefactor-init-project --write    # also expose the mutation tools over MCP
```

`gorefactor-init-project` writes:

- **Agent rules** — `CLAUDE.md` / `.cursorrules` / `AGENTS.md`, so Claude Code, Cursor,
  and AGENTS.md-based agents prefer gorefactor over hand-editing `.go`.
- **MCP config** — `.mcp.json` (Claude Code) and `.cursor/mcp.json` (Cursor).
- **Doctor gate** — a `.githooks/pre-commit` running `gorefactor doctor` (lint + build +
  test) plus `core.hooksPath`. The safety net: broken Go is caught before commit no
  matter which tool made the edit. Bypass one commit with `git commit --no-verify`.

Installed only the binaries (via `go install …@latest`)? Use the built-ins directly —
`gorefactor init-agent-rules --target all --mcp` writes the rules + `.mcp.json`, and any
pre-commit hook that runs `gorefactor doctor` gives you the gate.

**Batch / headless refactors.** For large or autonomous mechanical work, delegate a spec
to the agent — it runs the cheap model first and escalates only if it hands the task back:

```bash
gorefactor-delegate "add a ctx context.Context first param to Foo and update callers"
#   → runs on Haiku; on a punt (exit 3) escalates to Sonnet.
#     Needs ANTHROPIC_API_KEY and a clean git worktree (rolls back to it on punt).
```

### Exit codes

Commands use semantic exit codes so agents and CI can branch on the failure mode:

| Code | Meaning |
|------|---------|
| `0` | success |
| `1` | usage / argument error |
| `2` | target or pattern not found (semantic miss — retry with a different target) |
| `3` | parse / syntax rejection (snippet or file does not parse) |
| `4` | gate failure (`go build`/`go test` failed; used by `doctor` and `--gate`) |

Most mutation commands also accept `--dry-run` (preview without writing) and `--gate` (run build+test after the change, fail with code `4` if broken).

## Comparison to alternatives

### vs. gopls (Language Server)
- **gopls**: Great for interactive IDE refactoring (rename, extract, inline)
- **GoRefactor**: CLI-based, batch operations, LLM-integrated, resilient to code changes
- **Speed**: gopls cold-start = ~1.91s; gorefactor = ~30ms (60× faster for CLI bulk ops)
- **When to use gopls**: Single file, interactive in-editor refactoring
- **When to use GoRefactor**: Bulk refactoring, CI automation, LLM-driven changes, batch operations

### vs. golangci-lint
- **golangci-lint**: Comprehensive linting (security, style, complexity)
- **GoRefactor**: Structural refactoring + linting with autofixes
- **Overlap**: Both detect complexity, duplication, dead code
- **When to use golangci-lint**: Strict linting rules, security scanning, code style enforcement
- **When to use GoRefactor**: You want autofix + refactoring recommendations in one tool

### vs. go/analysis framework
- **go/analysis**: Ultra-flexible low-level framework (gopls, golangci-lint plugins use this)
- **GoRefactor**: High-level, out-of-the-box commands, no DSL to learn
- **When to use go/analysis**: Custom analysis rules, plugin architecture
- **When to use GoRefactor**: Pre-built refactoring tools, quick CLI automation

### vs. Manual refactoring (sed, awk, hand-edits)
- **Manual**: Flexible, but error-prone (missed imports, typos, line-number drift)
- **GoRefactor**: Safe-by-design (AST-aware, runs goimports, validates syntax)
- **Cost**: GoRefactor = 5 minutes to learn + 30 seconds to run; Manual = 30 minutes to get right + high error rate
- **When to use manual**: One-off 2-3 line change
- **When to use GoRefactor**: Anything more complex or repetitive

### vs. IDE refactoring (VS Code, GoLand)
- **IDE**: Great UX, interactive, mouse-driven
- **GoRefactor**: Scriptable, batch-capable, CI/CD integration, LLM-driven
- **When to use IDE**: Interactive refactoring, small changes, learning
- **When to use GoRefactor**: Large batch refactors, CI pipelines, LLM agents, complex patterns

## Real-world examples

### Example 1: Extract method with automatic inference

```bash
gorefactor extract payment.go 23 31 validateCardNumber
```

**Before:**
```go
func ProcessPayment(card *Card) error {
    // ... setup code ...
    
    // Lines 23-31: Validate card
    if len(card.Number) < 13 {
        return fmt.Errorf("invalid card number")
    }
    sum := 0
    for _, digit := range card.Number {
        sum += int(digit - '0')
    }
    if sum%10 != 0 {
        return fmt.Errorf("checksum failed")
    }
    
    // ... rest of function ...
}
```

**After:** Parameters (`card`) and returns (`error`) are automatically inferred:
```go
func validateCardNumber(card *Card) error {
    if len(card.Number) < 13 {
        return fmt.Errorf("invalid card number")
    }
    sum := 0
    for _, digit := range card.Number {
        sum += int(digit - '0')
    }
    if sum%10 != 0 {
        return fmt.Errorf("checksum failed")
    }
    return nil
}

func ProcessPayment(card *Card) error {
    if err := validateCardNumber(card); err != nil {
        return err
    }
    // ... rest ...
}
```

### Example 2: Find what needs refactoring

```bash
# See what's worth extracting
$ gorefactor recommend payment.go
[
  {
    "file": "payment.go",
    "functionName": "ProcessPayment",
    "blockStart": 23,
    "blockEnd": 38,
    "complexity": 8,
    "statementCount": 12,
    "reason": "High complexity, good extraction candidate"
  },
  // ... more recommendations
]

# See which files are oversized
$ gorefactor analyze-file-sizes .
Large files (>500 lines):
  handlers.go (687 lines) — suggest splitting by receiver: Handler, Router, Middleware
  service.go (612 lines) — suggest splitting by prefix: Payment*, Auth*, Cache*

# Auto-fix
$ gorefactor lint . --fix
Splitting handlers.go into handlers_handler.go, handlers_router.go, handlers_middleware.go
✓ handlers.go
✓ service.go
```

### Example 3: Batch refactoring with JSON plans

Extract the same validation pattern from 5 different places:

```bash
# Generate a plan
$ gorefactor suggest-plan payment.go --patterns > refactor-plan.json

# Review and execute
$ gorefactor orchestrate refactor-plan.json
Applied 5 operations in 1.2s
Refactor committed to .gorefactor/snapshot-20260524-151030 (use 'undo' to rollback)
```

### Example 4: Autonomous cleanup via LLM agent

```bash
# Find all issues and let the agent fix them
$ gorefactor-agent -campaign -max-iter 10
Step 1/5: Detected file-size violations in handlers.go
  → Splitting into 3 files...
Step 2/5: Found 2 duplicate blocks in service.go
  → Extracted to shared helper...
Step 3/5: Identified untested functions in middleware.go
  → Marked with t.Skip() for visibility...

✓ All issues fixed. Changes committed.
```

### Example 5: Find callers before refactoring

```bash
# Who calls this function? (helps plan renames/moves)
$ gorefactor find-callers ProcessPayment
handlers/payment.go:45   - ServeHTTP calls ProcessPayment
service/payment.go:12    - authorizeTransaction calls ProcessPayment
agent/payment_test.go:89 - TestPaymentFlow calls ProcessPayment

# Safe to rename/move? Check the 3 call sites.
```

## `gorefactor-agent` (summary)

Requires `OPENAI_API_KEY` or `ANTHROPIC_API_KEY` (or a local OpenAI-compatible endpoint via `-api-base`).

| Mode | Flag | Use when |
|------|------|----------|
| Agentic (default) | `-spec "..."` | Open-ended refactors; tool loop, up to 40 iterations by default |
| Interactive | `-spec "..." -interactive` | Pause after each tool for review/feedback |
| Single-shot | `-single-shot` | One constrained JSON plan (optional `-dry-run`) |
| Campaign | `-campaign` | Fix `gorefactor lint` findings autonomously |

`-max-iter` overrides the mode default (40 for agentic, 3 for single-shot). The agent’s `finish` gate runs **`go build` + `go test`** only. For lint + build + test, run **`gorefactor doctor`** yourself or in CI.

Before reaching for the agent, read [benchmark/FINDINGS.md](benchmark/FINDINGS.md) — for well-scoped structural tasks the direct CLI is strictly cheaper (~0 tokens vs 20–75K per task).

## Architecture & design

GoRefactor follows the **harness pattern** (Fowler): guides (feedforward—refuse to produce bad code) and sensors (feedback—lint rules with autofixes).

### Core packages

| Package | Responsibility |
|---------|-----------------|
| **parser/** | AST parsing → structured JSON (package info, functions, methods, types, interfaces). Foundation for all analysis. |
| **analyzer/** | Complexity scoring, extraction recommendations, dead code, duplicates, diff analysis, call graphs. Powers `recommend`, `inspect`, `lint` heuristics. |
| **orchestrator/** | JSON plan execution, semantic targeting (function names + code patterns + variable usage), undo snapshots under `.gorefactor/`. Enables resilient batch refactoring. |
| **cmd/gorefactor/** | CLI commands. 25+ `cmd_*.go` files (one per command). Extraction logic in `cmd_extract.go` (type-aware via `go/packages`). Direct-mutation commands here (create, insert, replace, delete, move, rename, extract, split, format). |
| **cmd/gorefactor-agent/** | LLM harness. Proposes operations (never edits `.go` directly). Supports OpenAI-compatible and Anthropic providers. Modes: agentic (40-step loop), single-shot, campaign (autonomous), interactive. Completion gate: `go build` + `go test` (not lint). |

**Design principle**: Use GoRefactor when the tool determines *where* and *how*; use Claude/LLM when it determines *what* to change. This minimizes token cost and keeps safety high (deterministic execution, not code generation).

### Token efficiency (critical for LLM agents)

Extract a 200-line function:
- **Gorefactor path**: LLM identifies the block (50 tokens) → tool extracts, infers params/returns (instant) = **99.5% token savings**
- **Manual edit path**: LLM reads file, outputs new function, new call site (1000+ tokens)

Batch refactoring (apply 1 pattern to 10 files):
- **Gorefactor path**: One plan (100 tokens) → tool applies 10 times (instant) = **80%+ savings**
- **Manual path**: Repeat the change 10 times in LLM output (1000+ tokens)

See [CLAUDE.md → Token Efficiency](CLAUDE.md#token-efficiency--operation-selection) for the decision matrix.

## Development & quality

**Quality gates:**
```bash
make check              # fmt, vet, golangci-lint, test (full pre-commit)
make test               # tests with race + coverage
./gorefactor doctor     # lint + go build + go test (final gate before shipping)
./gorefactor lint .     # Check for structural issues (file-size, duplicates, complexity, coupling, etc.)
```

**Contributor workflow:**
- Use `gorefactor` for `.go` file mutations (not Write/Edit). Ensures AST-aware changes and type safety.
- Pre-commit hook (optional): `ln -s ../../.githooks/pre-commit .git/hooks/pre-commit`
- See [CLAUDE.md](CLAUDE.md) for full developer guidance, harness patterns, and semantic targeting strategy.

**Reliability & benchmarks:**
- Second-tier agent (qwen2.5-coder 14b) achieves **80% success rate** on complex refactoring tasks
- **20% clean punts** (warm report, warm handoff to human—not errors)
- Mean time per task: **7 seconds**
- **Zero frontier tokens** (all work is local; frontier spend only for unresolved hand-offs)
- See [RELIABILITY.md](RELIABILITY.md) for full battery results.

## License

MIT License
