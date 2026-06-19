# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

**User-facing overview and install:** [README.md](README.md). **JSON plans:** [ORCHESTRATION_SYSTEM.md](ORCHESTRATION_SYSTEM.md).

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
| Replace a complete statement | `gorefactor replace <file> <Func> <old> <new>` |
| Replace partial text inside a function | `gorefactor replace-text <file> <Func> <old> <new>` |
| Replace a whole function body | `gorefactor replace-body <file> <Func> -` |
| Delete a function/method | `gorefactor delete <file> <Func> --safe` (checks callers first) |
| Inline a trivial function into its callers | `gorefactor inline <file> <Func>` |
| Rename an unexported symbol | `gorefactor rename <file> <old> <new>` |
| Add a field to a struct | `gorefactor add-field <file> <Struct> "<Name> <Type>" [--update-literals]` |
| Add/remove/rename a parameter (+ call sites) | `gorefactor change-signature <file> <Func> --add-param "n T"` |
| Flip a method receiver value↔pointer | `gorefactor change-receiver <file> <Type:Method> --pointer` |
| Set/replace a doc comment | `gorefactor set-doc <file> <decl> -` |
| Scaffold a table-driven test | `gorefactor add-test <file> <Func>` |
| Generate an interface from a type | `gorefactor extract-interface <file> <Type> <Iface>` |
| Stub out unimplemented interface methods | `gorefactor implement-interface <file> <Type> <Iface>` |
| Split a file that grew too large | `gorefactor split <file>` |
| Batch edits all-or-nothing | `gorefactor txn` (single undo unit) |
| Structural search by AST pattern | `gorefactor search-ast '<pattern>'` (`$_` wildcard) |
| Token-cheap file shape (bodies elided) | `gorefactor skeleton <file>` |
| LLM context pack for a symbol | `gorefactor context <Symbol>` |
| Call tree (callees/callers) | `gorefactor callgraph <Func> [--callers]` |
| Diff exported API vs a git ref | `gorefactor api-diff [ref]` |
| Tests affected by current changes | `gorefactor test-affected [--run]` |
| Check what calls a function (before refactor) | `gorefactor find-callers <Func>` |
| Check where a symbol is used | `gorefactor find-uses <Symbol>` |
| Find interface implementations | `gorefactor find-implementations <Iface>` |
| Extract a block to a new function | `gorefactor extract <file> <startLine> <endLine> <methodName>` |
| Find extraction candidates (concise) | `gorefactor recommend <file> --short` |
| Detect file-size / duplicate / extract issues | `gorefactor lint .` |
| Autofix file-size issues | `gorefactor lint . --fix` |
| Final gate (lint + build + test) | `gorefactor doctor` |
| One-page file summary | `gorefactor inspect <file>` |

**When `Edit`/`Write` is OK**: non-Go files (Markdown, YAML, JSON, Makefile, go.mod), git operations, completely-new packages with multiple files where stdin-pipe friction outweighs the benefit. For .go file mutations, fall back to `Edit` only when none of the above commands apply and document why.

**Receiver-method syntax**: methods are referenced as `Receiver:Method` everywhere (e.g. `CodeInserter:InsertCode`). Pointer receivers work without `*` in the locator.

**Stdin convention**: any command that takes content accepts `-` as the last argument to read from stdin (UNIX convention). This avoids quoting issues with multi-line code.

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
- `recommend <file.go>`: JSON of extractable code blocks (with complexity scores)
- `inspect <file.go>`: One-page human summary (decls, sizes, lint hints, extraction candidates)
- `analyze-diff <diff.patch>`: Generate a refactoring plan from a git diff
- `analyze-file-sizes <dir>`: Find files over the size limit with extraction hints
- `find-callers <Func|Receiver:Method> [--in path] [--json]`: All callers of a target
- `find-uses <Symbol|Receiver:Method> [--in path] [--json]`: All uses of a symbol
- `find-implementations <Interface> [--in path] [--json]`: Types that satisfy an interface
- `find-package-deps [dir] [--json]`: Package dependency graph and circular-import detection
- `suggest-plan <file.go> [--output plan.json] [--json] [--patterns]`: Suggested refactoring plan for a file
- `callgraph <Func|Receiver:Method> [--callers] [--depth N] [--json]`: Transitive call tree (callees by default, callers with `--callers`)
- `context <Symbol|Receiver:Method> [--budget N] [--json]`: One-shot LLM context pack — definition, callers, signature types, tests
- `skeleton <file.go> [--json]`: File with function bodies elided — token-cheap file shape
- `search-ast <pattern> [--in path] [--json]`: Structural search; match a Go statement/expression pattern (`$_` is a wildcard)
- `api-diff [ref] [--json]`: Diff the exported API surface of the working tree against a git ref (default HEAD)
- `review [ref] [--json]`: Structural quality review of changed functions vs a git ref
- `test-affected [base] [--run] [--json]`: Map changed files (vs git base, default HEAD) to affected packages and their tests
- `architect [dir] [--suggest] [--output path]`: Generate a starter `go-arch-lint.yml` from the import graph
- `history [--json]`: List the journal of mutation operations (most recent last)

**Mutation (direct CLI — no orchestrator JSON needed)**
- `create <path> [content|-]`: Create a new .go file (auto-runs goimports). `-` reads stdin.
- `insert <file> <at-end|at-beginning|before:Func|after:Func|inside:Func> [content|-]`: Insert code.
- `replace <file> <Func|Receiver:Method> <old-stmt> <new-stmt>`: AST-aware replacement (pattern must be a complete statement).
- `replace-text <file> <Func|Receiver:Method> <old-text> <new-text>`: Literal text replace inside a function body (use this when the pattern isn't a full statement).
- `replace-body <file> <Func|Receiver:Method> [content|-]`: Replace a function/method body wholesale with new statements.
- `delete <file> <Func|Receiver:Method> [--safe]`: Delete a declaration; `--safe` checks callers first.
- `rename <file> <old> <new>`: Rename unexported symbol across the package (use gopls for exported).
- `move <source-file> <Func|Receiver:Method> <dest-file>`: Move a declaration between files.
- `inline <file> <Func>`: Inline a simple function into its call sites and delete it (refuses anything complex).
- `add-field <file> <Struct> "<Name> <Type> [tag]" [--after F] [--update-literals]`: Add a struct field; optionally rewrite positional literals to keyed form.
- `change-signature <file> <Func|Receiver:Method> (--add-param "n T" [--position N] [--call-value EXPR] | --remove-param <name|index> | --rename-param <old> <new>)`: Change a signature and update all call sites.
- `change-receiver <file> <Type:Method> --pointer|--value`: Switch a method's receiver between value and pointer form.
- `set-doc <file> <decl> [content|-]`: Set or replace the doc comment on a top-level declaration.
- `add-test <file> <Func|Receiver:Method>`: Generate a table-driven test scaffold for an exported function/method.
- `extract-interface <file> <Type> <IfaceName>`: Generate an interface declaration from a type's exported method set.
- `implement-interface <file> <Type> <Iface>`: Generate compiling method stubs for every unimplemented interface method.

**Automation**
- `lint [path] [--fix] [--json] [--max N] [--fail-only]`: Structural linter, 25 default rules (canonical list in `cmd/gorefactor/lint_registry_test.go`):
  - *size/structure*: `file-size`, `long-function`, `deep-nesting`, `complexity`, `extract-candidate`
  - *duplication*: `duplicate-block`, `duplicate-bare-sentinel`
  - *design smells*: `god-object`, `large-class`, `fat-interface`, `excessive-params`, `excessive-returns`, `data-clumps`, `type-switch`, `premature-abstraction`, `high-coupling`
  - *error handling*: `error-not-wrapped`, `if-err-log-return`, `wrap-log-return`, `wrap-bridge-log-return`
  - *coverage*: `untested-function`, `untested-package`
  - *dead code*: `dead-code`
  - *external*: `golangci-lint`, `arch-violation`
  - `--fix` autofixes the three rules with a single safe transform: `file-size` (via `split`), `dead-code` (delete unreferenced decls), `error-not-wrapped` (wrap with `fmt.Errorf(... %w)`). `--fail-only` prints only error-severity (blocking) issues.
- `doctor [dir] [--json]`: Aggregate gate — structural lint + `go build ./...` + `go test ./...`; non-zero on failure
- `split <file> [--max N] [--dry-run]`: Auto-split an oversized file by grouping methods on same receiver / functions sharing a CamelCase prefix.
- `format [path ...]`: In-process gofmt+goimports. Replaces external `goimports` dependency.
- `txn`: Apply a batch of mutation commands transactionally (all-or-nothing, single undo unit).
- `init-agent-rules [--target claude.md|cursor|agents.md|all]`: Write the gorefactor agent-rules snippet into CLAUDE.md / `.cursorrules` / AGENTS.md.

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
./gorefactor doctor     # lint + build + test gate

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

The `finish` and `run_gate` tools call **`runGate`**: `go build ./...` then `go test ./...` in the target module. They do **not** run `gorefactor lint`. For lint + build + test, run **`gorefactor doctor`** manually or in CI after agent work.

**Analysis-only tasks** (find callers/uses, "where is X") end via the **`report`** tool, which returns the answer and finishes *without* the build/test gate — no code changed, so the gate is irrelevant. The agent's tool catalog also exposes **`move_function`** (top-level funcs) alongside `move_method`; both were previously dispatchable but unadvertised, which caused the junior to punt function-move and find-callers tasks. When delegating to `gorefactor-agent`, the junior can now handle function moves and analysis questions, not just method moves — but a well-scoped analysis query is still cheapest run directly as `gorefactor find-callers`/`find-uses` (see the Decision Matrix; the agent path costs 20K+ tokens, the CLI ~0).

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
