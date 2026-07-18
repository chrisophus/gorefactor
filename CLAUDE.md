# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

**User-facing overview and install:** [README.md](README.md). **JSON plans:** [ORCHESTRATION_SYSTEM.md](ORCHESTRATION_SYSTEM.md). **Doctor redesign plan:** [docs/doctor-design-plan.md](docs/doctor-design-plan.md).

## Project Overview

GoRefactor is a command-line tool for analyzing and refactoring Go code. It focuses on method extraction and intelligent code analysis through a sophisticated JSON-based orchestration system. The tool provides both interactive commands and batch refactoring capabilities through resilient, semantic-based code targeting.

## Using gorefactor instead of Write/Edit on .go files

**Default rule for this repo**: when modifying any `.go` file, prefer a gorefactor command over `Write` or `Edit`. This is the project's harness — gorefactor parses the AST, infers types, runs goimports, and only writes back well-formed code, so the failure mode is "command rejects the change" rather than "file silently breaks." It's also far cheaper in tokens.

Mapping of common edits to commands (run `./gorefactor` for the full list):

| Want to… | Use |
|----------|-----|
| Create a new .go file | `gorefactor create <path> -` (reads stdin) |
| Add a function/type to a file | `gorefactor insert <file> at-end -` |
| Add a function right after another | `gorefactor insert <file> after:Func -` |
| Add a helper inside a function | `gorefactor insert <file> inside:Func -` |
| Move a function/method to a new file | `gorefactor move <src> <Func> <dest>` |
| Replace old→new in a function (auto statement-or-text) | `gorefactor edit <file> <Func> <old> <new>` |
| Replace a complete statement (explicit scope) | `gorefactor replace <file> <Func> <old> <new>` |
| Replace partial text inside a function (explicit scope) | `gorefactor replace-text <file> <Func> <old> <new>` |
| Replace a whole function body | `gorefactor replace-body <file> <Func> -` |
| Delete a function/method | `gorefactor delete <file> <Func> --safe` (checks callers first) |
| Inline a trivial function into its callers | `gorefactor inline <file> <Func>` |
| Extract an expression to a named local variable | `gorefactor extract-var <file> <Func> <expr> <name> [--all]` |
| Extract a constant expression to a named local const | `gorefactor extract-const <file> <Func> <expr> <name> [--all]` |
| Rename an unexported symbol | `gorefactor rename <file> <old> <new>` |
| Add a field to a struct | `gorefactor add-field <file> <Struct> "<Name> <Type>" [--update-literals]` |
| Add/remove/rename a parameter (+ call sites) | `gorefactor change-signature <file> <Func> --add-param "n T"` |
| Flip a method receiver value↔pointer | `gorefactor change-receiver <file> <Type:Method> --pointer` |
| Set/replace a doc comment | `gorefactor set-doc <file> <decl> -` |
| Add a `case` to a switch | `gorefactor insert-switch-case <file> <Func> <case-expr> -` |
| Add an element to a map/slice literal | `gorefactor insert-map-entry <file> <VarOrFunc> -` |
| Edit text inside a string literal | `gorefactor replace-in-literal -- <file> <old> <new>` |
| Hoist function-local regexp compiles | `gorefactor hoist-regexp <file> [Func]` |
| Scaffold a table-driven test | `gorefactor add-test <file> <Func>` |
| Generate an interface from a type | `gorefactor extract-interface <file> <Type> <Iface>` |
| Stub out unimplemented interface methods | `gorefactor implement-interface <file> <Type> <Iface>` |
| Split a file that grew too large | `gorefactor split <file>` |
| Batch edits all-or-nothing | `gorefactor txn` (single undo unit) |
| Structural search by AST pattern | `gorefactor search-ast '<pattern>'` (`$_` wildcard) |
| Token-cheap file shape (bodies elided) | `gorefactor skeleton <file>` |
| LLM context pack for a symbol | `gorefactor context <Symbol>` |
| Call tree (callees/callers) | `gorefactor callgraph <Func> [--callers]` |
| Score change-impact before a refactor | `gorefactor blast-radius <Func>` |
| Diff exported API vs a git ref | `gorefactor api-diff [ref]` |
| Tests affected by current changes | `gorefactor test-affected [--run]` |
| Check what calls a function (before refactor) | `gorefactor find-callers <Func>` |
| Check where a symbol is used | `gorefactor find-uses <Symbol>` |
| Find interface implementations | `gorefactor find-implementations <Iface>` |
| Extract a block to a new function | `gorefactor extract <file> <startLine> <endLine> <methodName>` (`--allow-returns` lifts return-bearing blocks into a `(results..., done bool)` helper) |
| Find extraction candidates (concise) | `gorefactor recommend <file> --short` |
| Detect file-size / duplicate / extract issues | `gorefactor lint .` |
| Autofix file-size issues | `gorefactor lint . --fix` |
| Autofix, reverting any fix that breaks build/test | `gorefactor lint . --fix --verify` |
| Aggressive autofix (more rules, every fix gated) | `gorefactor lint . --fix --verify --fix-level aggressive` |
| Audit how much of the diff went through gorefactor | `gorefactor adherence [--since <ref>]` |
| Final gate (lint + golangci-lint + build + test) | `gorefactor doctor` |
| Merged health report, only-new-findings vs a ref | `gorefactor doctor --report [--base <ref>] [--scoped]` |
| Declare a deliberate API change (passes the apidiff gate) | `gorefactor intent api-change <scope> <reason>` |
| One-page file summary | `gorefactor inspect <file>` |

**When `Edit`/`Write` is OK**: non-Go files (Markdown, YAML, JSON, Makefile, go.mod), git operations, completely-new packages with multiple files where stdin-pipe friction outweighs the benefit. For .go file mutations, fall back to `Edit` only when none of the above commands apply and document why.

**Receiver-method syntax**: methods are referenced as `Receiver:Method` everywhere (e.g. `CodeInserter:InsertCode`). Pointer receivers work without `*` in the locator.

**Stdin convention**: any command that takes content accepts `-` as the last argument to read from stdin (UNIX convention). This avoids quoting issues with multi-line code.

**End-of-flags (`--`)**: pass a bare `--` to make every following argument positional (POSIX convention). Needed when a value starts with `-` — e.g. `replace-in-literal -- <file> "- old item" "- new item"`. Put any `--json`/`--dry-run`/`--gate` flags *before* the `--`.

## Harness pattern

gorefactor itself is structured as a harness in the sense of [Fowler's harness-engineering article](https://martinfowler.com/articles/harness-engineering.html):

- **Guides** (feedforward): the direct-op commands (`create`/`insert`/`replace`/etc.) refuse to produce malformed Go because they parse before they write. The LLM cannot accidentally introduce a syntax error via these paths.
- **Sensors** (feedback): `lint` aggregates 25 structural rules (size, duplication, design smells, error handling, coverage, dead-code, arch) and (where safe) autofixes `file-size` / `dead-code` / `error-not-wrapped` via `--fix`. Run it as a final gate after a refactor batch — anything not under control surfaces here.

When adding new capabilities to gorefactor, add a corresponding lint rule (sensor) so the agent self-detects when the new rule has been violated, and an autofix path (guide → sensor → autofix) when there's a single safe transformation.

## Architecture

### High-Level Design

The codebase is organized into library packages and command entry points:

1. **Parser** (`parser/`)
   - Low-level AST analysis of Go files
   - Extracts package info, imports, functions, methods, structs, interfaces
   - Output: Structured representation of Go code in JSON format
   - Used as the foundation for all other packages

2. **Analyzer** (`analyzer/`)
   - Code complexity analysis and block extraction recommendations
   - **DiffAnalyzer**: Translates git diffs into refactoring plans
   - Recommends extraction candidates based on configurable complexity rules
   - Key metrics: control structures, statement count, variable usage, error handling paths
   - Can analyze specific functions or entire files

3. **Orchestrator** (`orchestrator/`)
   - Executes refactoring plans defined in JSON format
   - Implements resilient semantic targeting strategies
   - Provides fallback mechanisms when targets change
   - Includes `CodeInserter` and operation handlers (extract, move, rename, insert, delete, etc.)
   - Generates JSON templates for common refactoring patterns
   - **Core feature**: Plans don't break when code changes—uses function names, patterns, and variable analysis instead of line numbers

4. **CLI** (`cmd/gorefactor/`)
   - Registers commands in `getCommands()` in `cmd/gorefactor/main.go`
   - **Extraction** is implemented in `cmd/gorefactor/cmd_extract.go` (type-aware block extraction via `go/packages`); used by the `extract` CLI command and orchestrator `extract_method` operations

5. **Agent** (`cmd/gorefactor-agent/`)
   - LLM harness: proposes tool calls, never writes `.go` source directly
   - Completion gate: `go build ./...` + `go test ./...` (see **Agent completion gate** below)

### Command Structure

Main commands in `cmd/gorefactor/main.go` (registered in `getCommands()`):

**Analysis (read-only sensors)**
- `parse <file.go>`: Parse a Go file → JSON structure
- `list-functions <file.go>`: List functions/methods with their **line counts**
- `recommend <file.go> [--reduce-complexity <Func> [--threshold N]] [--reduce-length <Func> [--max-lines N]]`: JSON of extractable code blocks (with complexity scores). `--reduce-complexity` switches to a threshold-driven mode: given an over-threshold function, it greedily picks the minimum set of top-level blocks to extract to bring the parent under `--threshold` (default 15), instead of surfacing micro-blocks. `--reduce-length` is the line-count analog (default threshold 75); both accept `--apply [--allow-returns]` to execute the extractions.
- `inspect <file.go>`: One-page human summary (decls, sizes, lint hints, extraction candidates)
- `analyze-diff <diff.patch>`: Generate a refactoring plan from a git diff
- `analyze-file-sizes <dir>`: Find files over the size limit with extraction hints
- `find-callers <Func|Receiver:Method> [--in path] [--json]`: All callers of a target
- `find-uses <Symbol|Receiver:Method> [--in path] [--json]`: All uses of a symbol
- `find-implementations <Interface> [--in path] [--json]`: Types that satisfy an interface
- `find-package-deps [dir] [--json]`: Package dependency graph and circular-import detection
- `suggest-plan <file.go> [--output plan.json] [--json] [--patterns]`: Suggested refactoring plan for a file
- `callgraph <Func|Receiver:Method> [--callers] [--depth N] [--json]`: Transitive call tree (callees by default, callers with `--callers`)
- `blast-radius <Func|Receiver:Method> [--in path] [--json]`: Change-impact score for a function/method — transitive callers, files/packages affected, and a composite risk score/level. Counts are name-based (over-approximate, like `callgraph`); use as a ranking signal before a refactor
- `context <Symbol|Receiver:Method> [--budget N] [--json]`: One-shot LLM context pack — definition, callers, signature types, tests
- `skeleton <file.go> [--json]`: File with function bodies elided — token-cheap file shape
- `search-ast <pattern> [--in path] [--json]`: Structural search; match a Go statement/expression pattern (`$_` is a wildcard)
- `api-diff [ref] [--json]`: Diff the exported API surface of the working tree against a git ref (default HEAD)
- `review [ref] [--json]`: Structural quality review of changed functions vs a git ref
- `test-affected [base] [--run] [--json]`: Map changed files (vs git base, default HEAD) to affected packages and their tests
- `architect [dir] [--suggest] [--output path]`: Generate a starter `go-arch-lint.yml` from the import graph
- `history [--json]`: List the journal of mutation operations (most recent last)
- `adherence [--since <ref>] [--json]`: Harness self-audit — of the changed `.go` files vs a git ref (default HEAD), what fraction of **modifications to existing files** went through a gorefactor op (attributed via the `history` journal, time-bounded) vs raw Write/Edit. **File creation is reported separately and excluded from the ratio** because `create` emits every line regardless of tool (token-neutral); the ratio measures where gorefactor's token value actually is. Advisory/heuristic (file-level, over-approximate) — a ranking signal, never a gate. Backs the `low-gorefactor-adherence` lint rule.

**Mutation (direct CLI — no orchestrator JSON needed)**
- `create <path> [content|-]`: Create a new .go file (auto-runs goimports). `-` reads stdin.
- `insert <file> <at-end|at-beginning|before:Func|after:Func|inside:Func> [content|-]`: Insert code.
- `edit <file> <Func|Receiver:Method> <old> <new>`: convenience dispatcher — tries statement-exact `replace` first, falls back to body-text `replace-text` when the pattern isn't a complete statement (prints which path it took). Additive over the explicit verbs below, which remain for pinning scope. Does **not** fold in `replace-body` (whole-body) or `replace-in-literal` (string contents) — those are distinct operations, not synonyms.
- `replace <file> <Func|Receiver:Method> <old-stmt> <new-stmt>`: AST-aware replacement (pattern must be a complete statement).
- `replace-text <file> <Func|Receiver:Method> <old-text> <new-text>`: Literal text replace inside a function body (use this when the pattern isn't a full statement).
- `replace-body <file> <Func|Receiver:Method> [content|-]`: Replace a function/method body wholesale with new statements.
- `delete <file> <Func|Receiver:Method> [--safe]`: Delete a declaration; `--safe` checks callers first.
- `rename <file> <old> <new>`: Rename unexported symbol across the package (use gopls for exported).
- `move <source-file> <Func|Receiver:Method> <dest-file>`: Move a declaration between files.
- `inline <file> <Func>`: Inline a simple function into its call sites and delete it (refuses anything complex).
- `extract-var <file> <Func|Receiver:Method> <expr> <name> [--all]`: Bind an expression inside a function to a new local variable (`name := expr`) and rewrite the occurrence. The binding is inserted into the *same block* as the occurrence (descending into nested `if`/`for`/`switch` bodies), so the expression is still evaluated at the same point — single-occurrence extraction is always behavior-preserving. `--all` rewrites every textual occurrence in the body, which additionally assumes the expression is pure and its inputs are unchanged (gate it with `--gate`). The pattern is matched as a whole Go expression (whitespace-insensitive), not raw text.
- `extract-const <file> <Func|Receiver:Method> <expr> <name> [--all]`: Like `extract-var` but emits `const name = expr`. Rejects expressions that syntactically can't be constant (calls, indexing, composite/func literals, address-of) or that reference a local variable/parameter; a package-level `var` operand still slips through the static check, so use `--gate` when unsure.
- `add-field <file> <Struct> "<Name> <Type> [tag]" [--after F] [--update-literals]`: Add a struct field; optionally rewrite positional literals to keyed form.
- `change-signature <file> <Func|Receiver:Method> (--add-param "n T" [--position N] [--call-value EXPR] | --remove-param <name|index> | --rename-param <old> <new>)`: Change a signature and update all call sites.
- `change-receiver <file> <Type:Method> --pointer|--value`: Switch a method's receiver between value and pointer form.
- `set-doc <file> <decl> [content|-]`: Set or replace the doc comment on a top-level declaration.
- `insert-switch-case <file> <Func|Receiver:Method> <case-expr> [body|-]`: Add a `case` to the first expression switch inside a function (before `default`, else at end). For non-statement code the statement-exact `replace` can't touch.
- `insert-map-entry <file> <VarOrFunc> <element|->`: Append an element to a composite literal — a package-level map/slice var, or the literal a func returns (e.g. a catalog builder). A trailing comma in the element is tolerated.
- `replace-in-literal <file> <old> <new>`: Replace text inside exactly one string literal (interpreted or raw), AST-scoped so surrounding code is never touched; reaches package-level literals (e.g. a prompt const) that `replace-text` (function-body-scoped) cannot. Use `--` before the args when `old`/`new` start with `-`.
- `remove-log-return <file> [--rule <name>] [--aggressive]`: Delete the redundant log statement next to an error-propagating return — the log-propagation lint family's autofix — and wrap a bare `return err` it uncovers. `--rule` limits to one of `if-err-log-return`/`wrap-log-return`/`wrap-bridge-log-return`; `--aggressive` also fixes log/return pairs separated by other statements.
- `wrap-sentinels <file> <Sentinel>`: Wrap every bare return of an `errors.New` sentinel with `fmt.Errorf("<context>: %w", Sentinel)` (the `duplicate-bare-sentinel` autofix).
- `hoist-regexp <file> [Func]`: Hoist function-local `regexp.MustCompile` calls with constant patterns to package-level vars (the `regexp-compile-in-func` autofix). Byte-range text surgery, so comments and formatting stay put; `regexp.Compile` is never touched (its error return would move).
- `wrap-errors <file> [<Func|Receiver:Method>]`: Rewrite bare `return err` inside `if err != nil` blocks to `fmt.Errorf` wrapping with context (the `error-not-wrapped` autofix); optionally scoped to one function.
- `reorder-funcorder <file>`: Reorder top-level declarations so each struct's constructor sits right after the struct and before its methods, exported methods precede unexported ones, and exported top-level functions (excluding constructors/`init()`) precede unexported ones (the `funcorder-constructor`/`funcorder-struct-method`/`funcorder-function` autofix). File-local — methods of the struct declared in other files aren't reordered.
- `add-test <file> <Func|Receiver:Method>`: Generate a table-driven test scaffold for an exported function/method.
- `extract-interface <file> <Type> <IfaceName>`: Generate an interface declaration from a type's exported method set.
- `implement-interface <file> <Type> <Iface>`: Generate compiling method stubs for every unimplemented interface method.

**Automation**
- `lint [path] [--fix [--verify]] [--baseline | --write-baseline] [--baseline-file PATH] [--json] [--max N] [--fail-only] [--info] [--verbose]`: Structural linter, 37 default rules (canonical list in `cmd/gorefactor/lint_registry_test.go`). By default `[info]` issues (e.g. `high-blast-radius`, `untested-*`) are hidden so actionable warnings aren't buried; `--info` shows them (collapsing per-file `high-blast-radius` into one summary line) and `--verbose` shows everything uncollapsed. A `lint.duplicate-ignore` list in `.gorefactor.yaml` excludes extra normalized-code patterns from `duplicate-block` (canonical error idioms like `if err != nil { return err }` are already excluded built-in).
  - **Baseline / ratchet mode** (adoption on an existing backlog): `--write-baseline` records the current findings to `.gorefactor-lint-baseline.json` (committable — it lives at the repo root, not the gitignored `.gorefactor/`); `--baseline` then suppresses everything in that snapshot and fails only on *new or worsened* issues, so a large legacy codebase can enforce "no new issues" without first paying the backlog down. Matching is line-number-independent (issue fingerprint = file + rule + message with digit runs normalized), so a finding that merely shifts when unrelated code is added above it stays suppressed; adding an Nth occurrence of an already-baselined finding surfaces the extra one. `--baseline-file PATH` overrides the path. The two modes are mutually exclusive.
  - Rules:
  - *size/structure*: `file-size`, `long-function`, `deep-nesting`, `complexity`, `extract-candidate`. `long-function` measures *logic* lines: composite literals and multi-line string literals count as data, so catalogs and prompt templates don't flag. Both `long-function` and `complexity` demote to info on *dispatch-table-shaped* functions (top-level switch/type-switch, independent cases, no fallthrough) when the per-branch re-score — everything outside the switch + switch frame + its worst single case (`analyzer.AnalyzeDispatch`) — is under threshold: a table is read one case at a time, and function-total metrics charge it per row; the finding stays warning when any single branch or the non-switch remainder carries the bulk.
  - *duplication*: `duplicate-block`, `duplicate-bare-sentinel`
  - *design smells*: `god-object`, `large-class`, `fat-interface`, `excessive-params`, `excessive-returns`, `data-clumps`, `type-switch`, `premature-abstraction`, `high-coupling`, `high-blast-radius`
  - *error handling*: `error-not-wrapped`, `if-err-log-return`, `wrap-log-return`, `wrap-bridge-log-return`
  - *ordering* (ports golangci-lint's `funcorder`, whose default-enabled checks are `constructor` and `struct-method`, plus its opt-in `function` check; the opt-in `alphabetical` check remains out of scope): `funcorder-constructor` (a struct's constructor must be declared before any of its methods — not required to be adjacent to the struct itself, though autofix canonicalizes it to right after), `funcorder-struct-method` (exported methods must all precede unexported ones), `funcorder-function` (top-level, receiver-less functions — excluding constructors and `init()` — must have exported ones before unexported ones)
  - *coverage*: `untested-function`, `untested-package`
  - *test hygiene* (gate integrity; from the react-doctor-adapted inventory in [docs/doctor-design-plan.md](docs/doctor-design-plan.md)): `vacuous-test` (a test with no assertion path can never fail — it games the build+test gate), `sleep-in-test` (`time.Sleep` as synchronization is flaky)
  - *shape-conditioned* (react-doctor inventory, step 4): `fatal-in-library` (`log.Fatal*`/`os.Exit` warn and `panic` is info in non-main packages — a library returns errors; package main and tests exempt)
  - *conc/lifecycle* (react-doctor inventory, step 5; package main exempt): `unstopped-ticker` (`time.NewTicker` never stopped in the declaring function nor handed to a caller), `naked-goroutine` (info; `go` statement with no visible context/done-channel/WaitGroup lifecycle signal). Context misuse is covered reuse-first by golangci's `contextcheck`/`containedctx` (enabled in `.golangci.yml`, mapped to the conc category by the doctor golangci substrate), not by a bespoke rule
  - *prop drilling*: `pass-through-param` (info; a parameter forwarded ≥3 in-package call layers without being used — name-based and conservative: shadowing bails out, methods out of scope)
  - *perf*: `regexp-compile-in-func` (constant-pattern `regexp.MustCompile`/`Compile` inside a function — recompiled on every call; `MustCompile` sites autofix via `hoist-regexp`), `string-concat-in-loop` (`+=` of provably-string values in a loop — use `strings.Builder`), `linear-search-in-loop` (info; equality scan nested in another loop — the rule can't see n, so small-slice scans keep it advisory)
  - *dead code*: `dead-code`
  - *external*: neither `go-arch-lint` nor golangci-lint is a `lint` rule — each runs as its own stage in `doctor` (self-skipping when the binary/config is absent), keeping `lint` fast and in-process. Run them standalone with `go-arch-lint check` / `golangci-lint run`.
  - *harness self-audit*: `low-gorefactor-adherence` (advisory; fires when too few existing-file edits went through gorefactor)
  - `--fix` autofixes the rules with a single safe transform: `file-size` (via `split`), `dead-code` (delete unreferenced decls), `error-not-wrapped` (wrap with `fmt.Errorf(... %w)`), `complexity` (extract sub-blocks), the three log-propagation rules `if-err-log-return`/`wrap-log-return`/`wrap-bridge-log-return` (via `remove-log-return`: delete the redundant log statement, wrap a bare `return err`; only adjacent log/return sites get an autofix — non-adjacent findings stay manual at this level), `duplicate-bare-sentinel` (via `wrap-sentinels`), `funcorder-constructor`/`funcorder-struct-method`/`funcorder-function` (via `reorder-funcorder`), and `regexp-compile-in-func` (via `hoist-regexp`; `MustCompile` sites only — hoisting `Compile` would move its error return). `--fail-only` prints only error-severity (blocking) issues.
  - `--fix-level aggressive` (requires `--fix --verify`) raises the autofix bar to transforms that are mechanical but not provably behavior-preserving, trading the "single safe transform" guarantee for the per-fix build+test gate: `long-function` and `extract-candidate` gain extraction-based autofixes (via `recommend --reduce-length --apply`, generated helper names), the `complexity`/`long-function`/`extract-candidate` extractions may lift return-bearing blocks (`extract --allow-returns`) but skip *vacuous* blocks — ones whose helper would itself trip the same rule (a whole-body switch, an over-threshold block) or whose suggested name needed a collision suffix (`processStmts2`) — because those extractions only relocate the finding, `remove-log-return` also fixes non-adjacent log/return pairs, and `dead-code` additionally flags exported top-level functions unreferenced anywhere in the module (never methods — reflection/interface dispatch can reach those without an in-module identifier). Out-of-module consumers of exported API are invisible to the scan; the verify gate and `undo` are the safety net, which is why the level cannot be enabled without them.
  - `--verify` (only with `--fix`) makes autofixes self-checking: the affected package directories are snapshotted, fixes are applied in batches of up to 8 (`defaultAutoFixBatchSize`), and `go build ./...` + `go test ./...` runs once per batch (doctor's gate minus lint). A green batch keeps every fix in it — one gate run for the whole group. A red batch is binary-searched (`bisectAutoFixBatch`): the batch is reverted, split in half, and each half is retried independently until the exact bad fix(es) are isolated and reverted while the good ones in the batch are kept. Good fixes are kept and journaled (so `undo` still works); reverted ones leave the tree untouched. This is the trust unlock for unsupervised bulk cleanup — the sensors are over-approximate (a `dead-code` symbol reached via reflection/build tags, a `split` that breaks a downstream package), and the gate is the backstop that catches those and rolls them back. Still slower than no verification (at least one build+test per batch, more when bisecting a red one) and human-path only. The summary line becomes `N applied, M reverted (gate failed), K failed to apply`.
- `doctor [dir] [--json] [--fix [--fix-level safe|aggressive]]`: Aggregate gate — structural lint + golangci-lint + go-arch-lint (each its own stage; skipped when the binary or config `.golangci.*`/`.go-arch-lint.*` is absent) + `go build ./...` + `go test ./...`; non-zero on failure. The golangci stage distinguishes "golangci-lint couldn't run at all" (bad/mismatched binary, config load error) from "it ran and found N issues": the former soft-skips (`ok`, `skipped, did not run: <reason>`) exactly like a missing binary — it can't be told apart from "clean" locally, so it must not block commits — while the latter hard-fails with an issue count. CI runs a known-good pinned golangci-lint and is the real enforcement backstop; `.claude/hooks/session-start.sh` best-effort installs that same pinned version (from the Makefile's `GOLANGCI_VERSION`) on every Claude Code web session so this stage has a working binary locally when the environment's network policy allows the download. `--fix` runs the same autofix pass as `lint --fix --verify` first (every fix is build+test gated and reverted individually on failure — always verified here, since doctor is itself the trust gate), then does a whole-tree `format` (gofmt+goimports) sweep so files no autofix rule touched still come out clean, reported together as the `autofix` stage — which never fails the gate itself (a fix "failing to apply", e.g. no extractable blocks, is a normal best-effort outcome, not build breakage; the lint/build/test stages below are the real gate) — then runs the rest of the gate against the resulting tree; `--fix-level aggressive` widens which fixes are attempted (see the `lint --fix-level` note above). `--report [--base REF] [--scoped] [--score]` switches to the diagnose engine from [docs/doctor-design-plan.md](docs/doctor-design-plan.md): substrates (structural lint, golangci-lint, apidiff, temporal, modtidy, plus full-run-only deadcode and govulncheck) merge into one Report, every finding is fingerprint-marked new or pre-existing against the base ref (default HEAD; base fingerprints are cached per commit SHA in `.gorefactor/`), generated/`walk:`-skipped files are filtered uniformly, and runs are journaled to `.gorefactor/doctor-history.jsonl`. Advisory-first: `--report` always exits zero. `--scoped` matches the agent gate's per-edit behavior — only the packages touched vs the base ref plus depth-1 reverse deps are analyzed (full-run-only substrates skip with a recorded status; `FixedCount` is omitted since a scoped run can't prove an absent finding fixed). `--score` adds a presentation-only 0–100 score on full runs (nothing gates on it). Weighting is value-tiered by rule, not severity alone — each rule contributes by how reliably fixing its findings is a *genuine* maintainability/changeability/testability/performance gain: defect rules (duplication, error handling, dead code, perf, gate integrity, lifecycle) count full severity weight; size/shape proxies (`long-function`, `complexity`, `excessive-params`, …) count half, because their counts can be lowered by code motion alone (wrapping a body in one helper, param-struct-then-unpack) — half weight keeps the pressure but halves the payoff of metric-shaped churn; conventions (`funcorder-*`) and context/ranking signals (`high-blast-radius`, `low-gorefactor-adherence`, heuristics that admit blindness) count zero; `untested-package` counts like a warning despite info severity — a package with zero tests is an actionable testability defect whose fix can't be faked (`untested-function` stays excluded as review-granularity). Tier membership is cross-checked against the rule registry by test. The programmatic API is `doctor.Diagnose` (package `doctor/`). Substrate notes: `temporal` (workflow-determinism, tmprl/error, gating) wraps Temporal's official `workflowcheck` binary when on PATH and falls back to an in-process AST scan of `workflow.Context` functions — modules without `go.temporal.io/sdk` trivially pass; `govulncheck` reports only symbol-level (reachable) vulns as sec/error and records itself unavailable — never a silent pass — when the vuln DB is unreachable; `deadcode` wraps x/tools deadcode `-json -test` (dead/warning); `modtidy` maps `go mod tidy -diff` to findings with a `go mod tidy` FixCmd plus info-severity single-use-dependency hints. `doctor install [--target claude.md|cursor|agents.md|all]` is the prevention loop: it appends doctor's rule expectations (generated live from the rule registry, sentinel-marked, idempotent) to agent context files. Shape detection is `doctor.DetectShape` (library vs binary dirs, Temporal/Kafka presence, Go version).
- `intent api-change <scope> <reason> | --list | --clear`: Declare a deliberate exported-API change so `doctor --report` and the agent's doctor gate pass it. Scope is a package dir or symbol prefix (`analyzer`, `analyzer.ComputeAPIDiff`) — matching respects `.`/`/` boundaries, so declarations can't blanket-cover unrelated symbols. Records live in `.gorefactor/intents.json`; clear them when the change ships.
- `split <file> [--max N] [--dry-run]`: Auto-split an oversized file by grouping methods on same receiver / functions sharing a CamelCase prefix.
- `format [path ...]`: In-process gofmt+goimports. Replaces external `goimports` dependency.
- `txn`: Apply a batch of mutation commands transactionally (all-or-nothing, single undo unit).
- `init-agent-rules [--target claude.md|cursor|agents.md|all] [--mcp|--mcp-only]`: Write the gorefactor agent-rules snippet into CLAUDE.md / `.cursorrules` / AGENTS.md; `--mcp` also merges a `.mcp.json` pointing an MCP client at `gorefactor mcp`.
- `mcp [--allow-write] [--allow-dirty]`: Run a stdio MCP server (official Go SDK) exposing gorefactor's read-only analysis commands as MCP tools plus `skeleton`/`inspect`/`context` as MCP resources. `--allow-write` additionally exposes the mutation guides (create/insert/replace/move/rename/delete/format/txn/undo) as destructive-annotated tools, gated on a clean git worktree (skip with `--allow-dirty`). See `docs/mcp-server-plan.md`.

**Plans**
- `orchestrate <plan.json>`: Execute a refactoring plan
- `exec`: Execute a single op from inline JSON or stdin
- `undo`: Roll back the last refactoring (uses snapshots in `.gorefactor/`)
- `generate-templates <dir>`: Generate example plan templates
- `repl`: Interactive REPL for step-by-step refactoring

## Token Efficiency & Operation Selection

**Core Principle**: Use GoRefactor for structural transformations where the LLM identifies _what_ to change and the tool determines _where_ and executes deterministically. Avoid operations where the LLM must read entire files and output significant code.

### When to Use GoRefactor (Token-Efficient)

These operations require minimal LLM context and produce no code output:

**Structural targeting operations** (no code I/O):
- ✅ **Move/copy functions** - `move_method`, `move_function`: Target by name, no code reading/writing
- ✅ **Delete code** - `delete_block`: Just needs location (function name, line range)
- ✅ **Rename symbols** - `rename_variable`, `rename_function`: Semantic targeting with find-and-replace
- ✅ **Simple insertions** - `insert_code` at known locations: `before_function`, `after_function`, at package level

**Analysis-driven operations** (LLM reads output, not input):
- ✅ **Method extraction** - LLM identifies which block, orchestrator extracts and infers parameters/returns
- ✅ **Apply consistent patterns** - Single plan targets multiple files; one LLM decision, many tool executions
- ✅ **Batch refactoring** - Process 10 similar changes with one orchestration plan

**Efficiency formula**:
```
Token savings = (1 - (complexity of planning / complexity of implementation)) × code_size
```
- Moving a 200-line function: Plan in 100 tokens, execute instantly (99.5% savings)
- Extracting a method: Identify block in 50 tokens, tool infers signature (95%+ savings)
- Applying pattern to 5 files: One plan, five executions (80%+ savings)

### When to Use Claude (Let the LLM Handle It)

These require semantic understanding and full-code generation:

**Logic-level changes** (needs reasoning):
- ❌ **Rewriting algorithms** - Requires understanding intent, evaluating tradeoffs, outputting new logic
- ❌ **Bug fixes** - Needs to understand what's wrong and why, often requires full context
- ❌ **New features** - Requires writing new code with domain logic
- ❌ **Complex refactoring** - Changing behavior while maintaining semantics requires human reasoning
- ❌ **Conditional edits** - "If X then do Y, else do Z" decisions need semantic judgment

**Context-dependent changes**:
- ❌ **Renaming for clarity** - LLM picks better names based on semantic meaning
- ❌ **Architectural changes** - Requires understanding design goals and tradeoffs
- ❌ **Error handling** - Adding proper error paths requires domain knowledge
- ❌ **Type changes** - Converting int to string needs understanding of implications

### Decision Matrix

| Operation | Token Cost | Tool | Reasoning |
|-----------|-----------|------|-----------|
| Move method to new file | ~5-10 tokens | GoRefactor | Target by name, no code I/O |
| Rename variable everywhere | ~5-10 tokens | GoRefactor | Semantic targeting, find-replace |
| Delete unused function | ~5 tokens | GoRefactor | Just needs location |
| Extract method (identify block) | ~20-50 tokens | GoRefactor + Claude | LLM identifies, tool extracts |
| Rewrite inefficient loop | ~500+ tokens | Claude | Full code read/write + reasoning |
| Fix race condition | ~200+ tokens | Claude | Needs semantic understanding |
| Add error handling | ~100+ tokens | Claude | Requires domain knowledge |
| Move function between packages | ~10 tokens | GoRefactor | Semantic targeting, tool handles imports |

### Workflow: Maximizing Token Efficiency

1. **Analyze with tool** (free): `./gorefactor recommend`, `analyze-diff` → get JSON recommendations
2. **LLM reviews briefly** (~50 tokens): Scan JSON, decide which operations to execute
3. **Create one plan** (~100 tokens): Batch multiple operations together
4. **Tool executes** (zero tokens): `orchestrate plan.json` applies all changes
5. **LLM verifies** (~100 tokens): Read test output, spot-check changes

Total for 5 changes: ~250 tokens. Doing it manually: 1000+ tokens.

### Examples

**✅ Efficient: Moving related functions to a new file**
```json
{
  "operations": [
    { "type": "move_method", "target": { "functionName": "Helper1" }, "newFile": "helpers.go" },
    { "type": "move_method", "target": { "functionName": "Helper2" }, "newFile": "helpers.go" },
    { "type": "move_method", "target": { "functionName": "Helper3" }, "newFile": "helpers.go" }
  ]
}
```
LLM: "These three functions belong together" (50 tokens) → Tool moves all three (instant)

**❌ Inefficient: Have LLM rewrite error handling**
```
"Rewrite all error handling to use wrapping instead of the old pattern"
```
LLM must: read entire file → understand all errors → write new code for each → output full file (500+ tokens)
Better: Have Claude write one corrected function, extract pattern, use GoRefactor to apply elsewhere

## Development Commands

### Quality Gates & Build

**All builds run quality checks first.** Use the Makefile for consistency:

```bash
# Setup development environment
make dev-setup          # Install tools + format code

# Full build: gorefactor only (runs test, lint, fmt, vet first)
make build
go build -o gorefactor-agent ./cmd/gorefactor-agent

# Run individual checks
make test               # Run tests with coverage
make lint               # Run golangci-lint
make fmt                # Format code
make vet                # Run go vet
make check              # Run all checks in sequence
./gorefactor doctor     # lint + golangci-lint + build + test gate
# make gate additionally enforces the lint ratchet: no new/worsened
# warning+ structural findings vs the committed .gorefactor-lint-baseline.json
# (also a CI step). After a cleanup wave, re-lock with
# `./gorefactor lint . --write-baseline` and commit the shrunken baseline.
# The ratchet is mechanically one-way: `lint --baseline-ratchet REF` fails if
# the baseline file gained a fingerprint or grew a count vs REF (make gate
# checks vs HEAD; CI checks PRs vs their base). Deliberate growth — e.g. a new
# lint rule baselining its backlog — is opted into visibly: locally
# BASELINE_GROWTH_OK=1, in CI a [baseline-growth] head-commit marker.

# Check code quality
make coverage           # Generate coverage report (HTML)
make ci                 # CI: All checks for pull requests

# Code analysis (using gorefactor)
make analyze-dir        # Find patterns and duplication
make find-symbol SYMBOL=FunctionName  # Find uses
make find-callers FUNC=FunctionName   # Find callers
```

### Quality Standards

GoRefactor uses **golangci-lint** for code quality with these standards:

- **Cyclomatic Complexity**: Max 15 (catch overly complex functions)
- **Code Duplication**: Flag blocks >100 lines
- **Error Checking**: Enforce error handling
- **Type Safety**: Catch type assertion errors
- **Security**: Use gosec for security issues
- **Simplification**: Identify unnecessary code

See `.golangci.yml` for all enabled linters.

### Pre-Commit Hooks

Automatic checks run before every commit:

```bash
# Install pre-commit hook
ln -s ../../.githooks/pre-commit .git/hooks/pre-commit

# Bypass hooks if needed (not recommended)
git commit --no-verify
```

### Testing

```bash
# Run all tests with coverage
go test ./... -v -race -coverprofile=coverage.out

# Run tests for specific package
go test ./analyzer -v
go test ./parser -v
go test ./cmd/gorefactor -v
go test ./orchestrator -v
go test ./cmd/gorefactor-agent -v

# Run specific test
go test -v -run TestAnalyzeBlock ./analyzer

# Watch tests on file changes (requires watchexec)
make watch-test
```

### Building

```bash
# Full build (runs quality checks first)
make build

# Quick build (skip checks - not recommended)
go build -o gorefactor ./cmd/gorefactor

# After building, run commands like:
./gorefactor parse path/to/file.go
./gorefactor analyze-diff diff.patch
./gorefactor orchestrate plan.json
```

### Continuous Integration

GitHub Actions automatically runs on push/PR:
- vet check (catch obvious bugs)
- golangci-lint (code quality)
- unit tests (with coverage upload)
- build verification

See `.github/workflows/ci.yml` for CI configuration.

## Key Architectural Concepts

### Semantic Targeting Strategy

The orchestrator doesn't rely on line numbers. Instead, targets are specified using multiple strategies that can be combined:

- **Function/method names**: `functionName`, `methodName`, `receiverType`
- **Code patterns**: `codePattern` (substring matching in code)
- **Variable usage**: `variableNames` (list of variables used in block)
- **Function calls**: `functionCalls` (list of functions called)
- **Control structures**: `controlStructures` (if, for, switch statements)
- **Context matching**: `beforePattern`, `afterPattern` (surrounding code)

This makes refactoring plans resilient to code changes—even if internal implementation details change, the semantic characteristics remain stable.

### Complexity Analysis

Recommendations use these metrics:

- **Statement Count**: Total number of statements in a block
- **Control Structures**: Number of if/for/switch statements (indicates complexity)
- **Error Handling Paths**: Branches for error cases
- **Return Count**: Number of return statements
- **Variable Dependencies**: Read/write variables and their dependencies

Extraction recommendations filter by configurable complexity bounds (default: 1-10 complexity, 3-50 statements).

### Fallback Strategies

When a target cannot be located, operations have fallback behavior:

- `skip`: Silently skip the operation
- `use_default`: Fall back to first function in file
- Custom error handling in conditions

### Code Insertion

The orchestrator includes `CodeInserter` for adding new methods/code:

- Insertion points: `before_function`, `after_function`, `inside_function`, `at_end`, `at_beginning`
- Handles proper formatting and location accuracy
- Used by plans that need to add new code rather than just refactor existing code

## Important Patterns and Conventions

### Extraction dependency analysis

Extraction (`cmd/gorefactor/cmd_extract.go` and orchestrator `extract_method`) identifies dependencies by analyzing:

1. **Read variables**: Variables read but not declared in block (become parameters)
2. **Write variables**: Variables written in block that are used after extraction (become returns)
3. **Internal variables**: Declared and used within block (become local to extracted method)

Returns are ordered: explicitly written variables first, then the final expression.

### Complexity Scoring

Complexity is scored in recommendations based on:

- Each control structure statement adds complexity
- Deep nesting increases the score
- Error handling paths add significant weight
- Used to filter which blocks are good extraction candidates

### JSON Plan Structure

All refactoring plans follow this structure:

```json
{
  "version": "1.0",
  "name": "plan_name",
  "description": "what this does",
  "operations": [
    {
      "type": "extract_method|inline_method|rename_variable|move_method|insert_code",
      "description": "what this operation does",
      "file": "path/to/file.go",
      "target": { /* targeting strategy */ },
      "parameters": { /* operation-specific params */ },
      "conditions": [ /* optional conditional execution */ ],
      "fallback": { /* optional fallback strategy */ }
    }
  ]
}
```

Conditions allow operations to execute only when code meets certain criteria (e.g., minimum complexity thresholds).

## Testing Philosophy

- Each package has corresponding `_test.go` files
- Tests verify parsing accuracy, recommendation logic, extraction correctness, and orchestration behavior
- Use `go test` to validate changes
- Both unit tests and integration-style tests (e.g., full orchestration workflows)

## Git Workflow

- Use a feature branch; commit with clear messages; open PRs against `main`
- Pre-commit hook (optional): `ln -s ../../.githooks/pre-commit .git/hooks/pre-commit`

## Interactive Refactoring with gorefactor-agent

The repository includes `gorefactor-agent`, an agentic driver that uses an LLM to iteratively work through refactoring requests. The agent proposes changes, executes them via GoRefactor, receives feedback, and refines until complete—providing interactive, semi-autonomous refactoring.

### Operating modes

**1. Agentic mode (default)**

The LLM iteratively works on your refactoring spec using tool calls:

```bash
# From command-line text
./gorefactor-agent -spec "extract the payment validation logic into a separate method"

# From a file
./gorefactor-agent -spec @refactoring-request.txt

# With custom model provider
./gorefactor-agent -spec "..." -provider openai -model gpt-4o-mini
./gorefactor-agent -spec "..." -provider anthropic -model claude-opus-4-7
```

The agent will:
- Parse your refactoring request
- Propose specific GoRefactor operations
- Execute them via tool calls
- Get feedback from GoRefactor and code analysis
- Refine and iterate (up to 24 iterations by default)
- Summarize changes when complete

**2. Single-shot mode**

Generate a complete refactoring plan in one step—useful for simple, well-scoped tasks:

```bash
# Generate and preview without applying
./gorefactor-agent -spec "extract X" -single-shot -dry-run

# Then apply if the preview looks good
./gorefactor-agent -spec "extract X" -single-shot
```

**3. Campaign mode**

Sensor-driven autonomous mode: the agent analyzes GoRefactor's linter findings and autonomously fixes issues without needing a refactoring spec:

```bash
# Uses gorefactor lint to find issues, then fixes them
./gorefactor-agent -campaign
```

Exits with:
- Status 0: All fixes applied and committed
- Status 3: Punted (handed work back - requires human judgment)
- Status 1: Fatal error

**4. Interactive mode** (flag on agentic loop)

Pauses the agentic loop after each tool execution to let you review results and provide feedback:

```bash
./gorefactor-agent -spec "extract payment validation" -interactive
```

The agent pauses and shows you what it did, then prompts for your decision:

```
── step 2/24 ──
  → find_references
    references to PaymentService found in 3 places:
      payment.go:45
      handlers.go:12
      integration_test.go:88

  Continue? [c/f/r/s/a/?] >
```

**Interactive Commands**:
- `c` - **Continue** (accept this step and proceed to next)
- `f <text>` - **Feedback** (provide guidance: "Also handle timeout cases")
- `r` - **Review** (show `git diff` of changes so far)
- `s` - **Stop** (gracefully punt and rollback all changes)
- `a` - **Auto-continue** (resume full automation, stop pausing)
- `?` or `help` - Show help message
- `<enter>` - Same as `c`

When you provide feedback with `f`, it's incorporated into the agent's conversation history, guiding its approach for the next step. Use this mode for:
- Complex refactorings where you want to steer the agent's decisions
- Learning how the agent approaches problems
- Verifying changes step-by-step before they're applied
- Stopping early if the agent goes off track

### Common Options

```bash
# Model selection
-provider openai|anthropic        # LLM provider (default: openai)
-model <name>                     # Model name (default: gpt-4o-mini)
-api-base <url>                   # Custom API endpoint (for local models, proxies)

# Iteration control
-max-iter N                        # Max iterations (0 = mode default: 24 for agentic, 3 for single-shot)
-budget N                          # Token budget (prompt+completion); on exhaustion the agent stop-and-summarizes via a structured punt instead of wandering (0 = unlimited). In campaign mode it is the aggregate cap across findings.

# Debugging and inspection
-verbose                           # Show model reasoning and intermediate steps
-print-prompt                      # Preview the assembled prompt without calling the model
-dry-run                          # (single-shot only) Preview changes without applying

# Safety and flexibility
-dir <path>                        # Target Go module directory (default: .)
-allow-dirty                       # Skip the clean-git-worktree precondition
-single-shot                       # Use single-shot constrained-plan path (required for providers without tool-calling)
-interactive                       # (agentic mode only) Pause after each step for user feedback
-no-schema                         # (single-shot only) Disable JSON-schema enforcement
```

### Examples

**Interactive extraction**:
```bash
./gorefactor-agent -spec "extract the validateOrder function's business logic into a checkOrderValidity helper"
# Agent iterates, asks GoRefactor questions, refines the extraction
```

**Autonomous cleanup**:
```bash
./gorefactor-agent -campaign -max-iter 10
# Agent finds file-size issues and automatically splits them
```

**Preview before applying**:
```bash
./gorefactor-agent -spec "rename handleRequest to processRequest" -single-shot -dry-run
# Shows the exact changes GoRefactor would make
```

**Using a specific model**:
```bash
# OpenAI-compatible endpoint (e.g., local model server)
./gorefactor-agent -spec "..." -provider openai -api-base http://localhost:8000 -model mistral-7b

# Anthropic's API
./gorefactor-agent -spec "..." -provider anthropic -model claude-opus-4-7
```

### How It Works

The agent:
1. Parses your refactoring spec (or discovers issues via `gorefactor lint` in campaign mode)
2. Uses tool calls to run GoRefactor analysis commands (parse, recommend, find-callers, etc.)
3. Reads the output and decides which GoRefactor operations to execute
4. Applies operations via tool calls
5. Analyzes results and decides if more iterations are needed
6. Commits changes when done

The model never directly edits code—all mutations flow through deterministic GoRefactor commands, so the failure mode is "command rejects the change" rather than "malformed Go file."

### Agent completion gate

The `finish` and `run_gate` tools call **`runGate`**: `go build ./...`, then `go test ./...`, then a **scoped doctor pass** vs HEAD (golangci-lint + apidiff + temporal substrates; packages touched plus depth-1 reverse deps — the full-run-only deadcode and govulncheck substrates skip on scoped runs and execute on the campaign-completion full pass). The doctor leg is **advisory by default** (`-doctor-gate advisory`): new error-severity findings are reported in the gate output without blocking, per the design plan's advisory-first rollout. `-doctor-gate hard` makes them block, adds a full-repo doctor pass to campaign completion, and fail-fasts campaigns at start when a gating substrate can't run (`doctor.Preflight`); `-doctor-gate off` disables the leg. Structural lint stays out of the agent gate (its findings are warning-severity by design); for the full merged view run **`gorefactor doctor --report`**, and **`gorefactor doctor`** remains the lint + golangci + build + test aggregate gate.

**Analysis-only tasks** (find callers/uses, "where is X") end via the **`report`** tool, which returns the answer and finishes *without* the build/test gate — no code changed, so the gate is irrelevant. The agent's tool catalog also exposes **`move_function`** (top-level funcs) alongside `move_method`; both were previously dispatchable but unadvertised, which caused the junior to punt function-move and find-callers tasks. When delegating to `gorefactor-agent`, the junior can now handle function moves and analysis questions, not just method moves — but a well-scoped analysis query is still cheapest run directly as `gorefactor find-callers`/`find-uses` (see the Decision Matrix; the agent path costs 20K+ tokens, the CLI ~0).

### Context-management & persistence surfaces

The agent loop implements the token-efficiency and cross-session pieces of the harness (see [docs/harness-token-efficiency.md](docs/harness-token-efficiency.md)):

- **Tool-output masking** (agentic modes only — single-shot keeps no growing transcript): tool results older than the last 3 assistant turns are replaced with a one-line stub at prompt-assembly time (raw transcript untouched), so stale outputs aren't re-sent every round. Input tokens dominate agentic cost.
- **Token budget** (`-budget N`, all modes including single-shot and campaign): before each round the loop stop-and-summarizes via a structured punt once cumulative tokens hit the ceiling, instead of spending past the accuracy plateau.
- **Persistent notes** (`.gorefactor/notes.md`, all modes): loaded into the system prompt at start, appended only via the `note` tool (categories `repo_fact`/`failed_strategy`/`flaky_test`/`open_punt`) — single-shot mode reads notes but can't write them (no tool-calling surface). Trust them before re-discovering repo facts. Punts auto-record an `open_punt` note.
- **Failure corpus** (`.gorefactor/failures.jsonl`, all modes): every rejected mutation op, budget hit, and punt is appended (passive sensor; never gates). `.gorefactor/` is gitignored so it survives rollback. Feeds the Hashimoto mistake-cannot-recur loop: at the start of every **agentic** run the corpus is aggregated into a compact "KNOWN FAILURE MODES" block (top rejected tools with a representative digit-normalized reason, plus capability-gap/budget-hit counts) and appended to the system prompt, so the model sees which op shapes this repo has already rejected before it acts. The block is hard-bounded (≤ a handful of lines, trailing-window scan) and empty on a cold repo, so a fresh checkout pays nothing.

### Environment Setup

**API Keys**:
```bash
# OpenAI (for OpenAI-compatible models)
export OPENAI_API_KEY="sk-..."

# Anthropic
export ANTHROPIC_API_KEY="sk-ant-..."
```

**Local Models**:
If using a local model server:
```bash
./gorefactor-agent -spec "..." \
  -provider openai \
  -api-base http://localhost:8000 \
  -model local-model-name
```

### When to Use the Agent

**Use agentic mode when**:
- ✅ Refactoring requests are complex or open-ended ("improve code organization")
- ✅ You want interactive iteration—model refines based on feedback
- ✅ You want to see the agent's reasoning (use `-verbose`)
- ✅ The task might require multiple coordinated steps

**Use single-shot mode when**:
- ✅ Task is simple and well-scoped ("rename function X to Y")
- ✅ You want guaranteed termination in 1-3 steps
- ✅ You want to preview with `-dry-run` before committing

**Use campaign mode when**:
- ✅ You want autonomous cleanup based on GoRefactor's linter rules
- ✅ No specific refactoring request—just "improve the code quality"
- ✅ You trust the linter rules and want hands-off execution

## GoRefactor Analysis Commands for Development

GoRefactor includes powerful analysis commands to understand code structure before making changes. These commands form the basis of how the agent works internally.

### Available Analysis Commands

**Cross-File Duplicate Detection**
```bash
./gorefactor analyze-dir ./pkg
# Finds duplicate code patterns across files
# Returns: JSON with DuplicateBlock entries showing impact and consolidation opportunities
```

**Symbol Tracking (Find-Uses)**
```bash
./gorefactor find-uses SymbolName [--in path] [--json]
# Shows all uses of a symbol: calls, reads, writes, definitions, parameters, returns
```

**Caller Analysis (Find-Callers)**
```bash
./gorefactor find-callers FunctionName [--in path] [--json]
./gorefactor find-callers Receiver:MethodName [--json]
# Lists all places that call a function or method
# Shows: direct calls, indirect (interface) calls, test calls
```

**Interface Implementations**
```bash
./gorefactor find-implementations InterfaceName [--in path] [--json]
# Shows all types that implement an interface
```

**Extract Candidates**
```bash
./gorefactor recommend ./file.go
# Returns JSON with ranked extraction opportunities
# Scores blocks by complexity, extractability, and impact
```

**Extraction Planning from Diffs**
```bash
./gorefactor analyze-diff changes.patch
# Generates a RefactoringPlan based on git diff
# Useful for understanding what refactoring a change implies
```

### Using Analysis During Development

**Before implementing new features**:

1. **Find existing patterns** - Check for similar code before writing new code
   ```bash
   ./gorefactor find-uses Parser
   # See how existing code analyses Go code
   ```

2. **Understand dependencies** - Know what depends on code you're changing
   ```bash
   ./gorefactor find-callers OldFunctionName
   # Lists all call sites before refactoring
   ```

3. **Check for duplication** - Find duplicate blocks before adding more
   ```bash
   ./gorefactor analyze-dir ./analyzer
   # Shows code patterns and consolidation opportunities
   ```

4. **Evaluate extractability** - Before extracting, verify complexity is appropriate
   ```bash
   ./gorefactor recommend ./large_file.go
   # Shows which blocks are good extraction candidates
   ```

5. **Find interface implementations** - Understand the type hierarchy
   ```bash
   ./gorefactor find-implementations Reader
   # Lists all types implementing Reader interface
   ```

### Analysis + Agent Workflow

The agent uses these analysis commands internally to:
1. **Gather context** - Run find-callers, find-uses to understand impact
2. **Propose operations** - Use recommend output to suggest extractions
3. **Verify safety** - Run analysis after changes to ensure nothing broke
4. **Iterate** - Get feedback from each analysis to refine the plan

You can use the same commands manually when debugging agent decisions or planning refactors yourself:

```bash
# Manual workflow: understand → plan → execute
./gorefactor find-callers PaymentValidator
./gorefactor recommend payment.go
# ... review recommendations ...
./gorefactor-agent -spec "extract the highlighted block into validatePayment"
```

### When to Use Analysis Commands

✅ **Use analysis commands when**:
- Starting a new implementation phase
- Before refactoring to understand impact
- Designing interfaces or abstractions
- Verifying assumptions about code structure
- Debugging agent decisions
- Planning batch refactorings

❌ **Skip analysis if**:
- Writing simple standalone code (test cases)
- Feature is entirely new with no dependencies
- Task is documentation, not code
- Already familiar with the code area

## Notes for Future Work

- The orchestrator is the primary user-facing feature for batch operations; individual commands are useful for one-off analysis
- Diff analysis (`analyze-diff`) is valuable for understanding what changed and generating corresponding refactoring plans
- The semantic targeting system is the key innovation—it enables refactoring plans to remain valid across code evolution
- Plans can include conditions to ensure operations only run when appropriate, increasing safety
