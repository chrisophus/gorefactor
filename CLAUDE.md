# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

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
| Delete a function/method | `gorefactor delete <file> <Func>` |
| Rename an unexported symbol | `gorefactor rename <file> <old> <new>` |
| Split a file that grew too large | `gorefactor split <file>` |
| Check what calls a function (before refactor) | `gorefactor find-callers <Func>` |
| Check where a symbol is used | `gorefactor find-uses <Symbol>` |
| Find interface implementations | `gorefactor find-implementations <Iface>` |
| Extract a block to a new function | `gorefactor extract <file> <startLine> <endLine> <methodName>` |
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
- **Sensors** (feedback): `lint` aggregates `file-size` / `duplicate-block` / `extract-candidate` / `untested-package` checks and (where safe) autofixes via `--fix`. Run it as a final gate after a refactor batch — anything not under control surfaces here.

When adding new capabilities to gorefactor, add a corresponding lint rule (sensor) so the agent self-detects when the new rule has been violated, and an autofix path (guide → sensor → autofix) when there's a single safe transformation.

## Architecture

### High-Level Design

The codebase is organized into four core packages:

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

3. **Extractor** (`extractor/`)
   - Performs actual method extraction on specified code blocks
   - Analyzes variable dependencies to determine method parameters and return types
   - Handles edge cases like multiple return values and variable reassignments
   - Modifies source code in-place

4. **Orchestrator** (`orchestrator/`)
   - Executes refactoring plans defined in JSON format
   - Implements resilient semantic targeting strategies
   - Provides fallback mechanisms when targets change
   - Includes code insertion capabilities for adding new code blocks
   - Generates JSON templates for common refactoring patterns
   - **Core feature**: Plans don't break when code changes—uses function names, patterns, and variable analysis instead of line numbers

### Command Structure

Main commands in `main.go` (registered in `getCommands()`):

**Analysis (read-only sensors)**
- `parse <file.go>`: Parse a Go file → JSON structure
- `list-functions <file.go>`: List functions/methods with their **line counts**
- `recommend <file.go>`: JSON of extractable code blocks (with complexity scores)
- `analyze-diff <diff.patch>`: Generate a refactoring plan from a git diff
- `analyze-file-sizes <dir>`: Find files over the size limit with extraction hints
- `find-callers <Func|Receiver:Method> [--in path] [--json]`: All callers of a target
- `find-uses <Symbol|Receiver:Method> [--in path] [--json]`: All uses of a symbol
- `find-implementations <Interface> [--in path] [--json]`: Types that satisfy an interface

**Mutation (direct CLI — no orchestrator JSON needed)**
- `create <path> [content|-]`: Create a new .go file (auto-runs goimports). `-` reads stdin.
- `insert <file> <at-end|at-beginning|before:Func|after:Func|inside:Func> [content|-]`: Insert code.
- `replace <file> <Func|Receiver:Method> <old-stmt> <new-stmt>`: AST-aware replacement (pattern must be a complete statement).
- `replace-text <file> <Func|Receiver:Method> <old-text> <new-text>`: Literal text replace inside a function body (use this when the pattern isn't a full statement).
- `delete <file> <Func|Receiver:Method>`: Delete a declaration.
- `rename <file> <old> <new>`: Rename unexported symbol across the package (use gopls for exported).
- `move <source-file> <Func|Receiver:Method> <dest-file>`: Move a declaration between files.

**Automation**
- `lint [path] [--fix] [--json] [--max N]`: Structural linter. Rules: `file-size`, `duplicate-block`, `extract-candidate`, `untested-package`. `--fix` autofixes `file-size` via `split`.
- `split <file> [--max N] [--dry-run]`: Auto-split an oversized file by grouping methods on same receiver / functions sharing a CamelCase prefix.
- `format [path ...]`: In-process gofmt+goimports. Replaces external `goimports` dependency.

**Plans**
- `orchestrate <plan.json>`: Execute a refactoring plan
- `exec`: Execute a single op from inline JSON or stdin
- `undo`: Roll back the last refactoring (uses snapshots in `.gorefactor/`)
- `generate-templates <dir>`: Generate example plan templates

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

# Full build (runs tests, lint, fmt, vet first)
make build

# Run individual checks
make test               # Run tests with coverage
make lint               # Run golangci-lint
make fmt                # Format code
make vet                # Run go vet
make check              # Run all checks in sequence

# Check code quality
make coverage           # Generate coverage report (HTML)
make ci                 # CI: All checks for pull requests

# Code analysis (using refactor-skill)
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
go test ./extractor -v
go test ./orchestrator -v

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

### Variable Analysis in Extraction

The extractor identifies dependencies by analyzing:

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

- Work on the designated feature branch (`claude/add-claude-documentation-0NBR0`)
- Commit changes with clear, descriptive messages
- Push to origin with `git push -u origin <branch-name>`
- The repository is at `/home/user/gorefactor`

## GoRefactor Skill for Intelligent Refactoring

The repository includes a dedicated skill for efficient code refactoring that Claude Code can use:

### Interfaces

**Go Binary (`skill-refactor`)**: Structured JSON interface for Claude Code integration
```bash
./skill-refactor analyze path/to/file.go        # JSON recommendations
./skill-refactor refactor path/to/file.go 3     # Auto-apply top 3 refactorings
./skill-refactor extract file.go <start> <end> <method>  # Extract specific block
```

**Bash Wrapper (`refactor-skill.sh`)**: Human-friendly CLI with colored output
```bash
./refactor-skill.sh analyze path/to/file.go     # Show extraction candidates
./refactor-skill.sh extract-best path/to/file.go
./refactor-skill.sh simplify path/to/file.go 3
```

### When to Use the Skill

**Use when** (token-efficient operations):
- ✅ Analyzing code structure to find extraction opportunities (tool reads, you review JSON)
- ✅ Extracting high-complexity blocks (LLM picks block, tool infers signature)
- ✅ Batch refactoring multiple files with same pattern (one decision, many executions)
- ✅ Finding complexity hotspots (tool analyzes, you decide priorities)

**Avoid when** (better for LLM):
- ❌ Renaming for semantic clarity - LLM understands intent better
- ❌ Changing code logic or behavior - Requires reasoning
- ❌ Architectural decisions - Needs business context
- ❌ Complex interdependencies - Better with human judgment
- ❌ Test code - Often needs domain-specific knowledge

### How It Works

The skill:
1. Analyzes code complexity using metrics (control structures, statements, variable dependencies)
2. Ranks extraction candidates by priority (1-10 scale)
3. Generates intelligent method names based on code characteristics
4. Optionally applies safe, high-impact refactorings automatically

Priority factors:
- Sweet spot complexity (3-10)
- Clear inputs/outputs (has read and write variables)
- Maintainable size (≤ 20 statements)
- Extractability (valid AST, proper dependencies)

### Examples

**Analyze a file**:
```bash
./skill-refactor analyze service.go
# Output: JSON with priority-ranked extraction candidates
```

**Auto-refactor the top 3 candidates**:
```bash
./skill-refactor refactor service.go
# Modifies file in-place, applies highest-value extractions
```

**Extract a specific block**:
```bash
./skill-refactor extract service.go 45 62 validatePayment
```

### Integration

The skill is designed for Claude Code workflows:
1. Call `analyze` to find refactoring opportunities
2. Review JSON output to understand recommendations
3. Use `refactor` to apply automatically, or `extract` for specific blocks
4. Verify the results maintain intended behavior

For detailed documentation, see `SKILL_REFACTOR.md`.

## Analysis Tools for Development

GoRefactor includes built-in analysis tools (Phases 1-3) that help understand code structure during implementation:

### Available Analysis Tools

**Phase 1: Cross-File Duplicate Detection**
```bash
./gorefactor analyze-dir ./src
# Finds duplicate code patterns across files
# Returns: DuplicateBlock with impact scores and consolidation recommendations
```

**Phase 2: Find-All-Uses (Symbol Tracking)**
```bash
# Use the analyzer package directly in code
# Tracks: calls, reads, writes, definitions, parameters, returns
# Enables: understanding symbol dependencies before refactoring
```

**Phase 3: Find-Callers (Who Calls What)**
```bash
# Use the call analyzer for caller analysis
# Shows: direct calls, indirect (interface) calls, test calls
# Enables: understanding impact before renaming or moving functions
```

### Using Analysis During Implementation

Before implementing new features, use analysis to:

1. **Find existing patterns** - Don't duplicate what already exists
   ```bash
   ./refactor-skill.sh analyze-dir ./analyzer
   # Shows code patterns and opportunities
   ```

2. **Understand dependencies** - Know what depends on code you're changing
   ```bash
   ./refactor-skill.sh find-callers FunctionName
   # Lists all places that call a function
   ```

3. **Check for duplication** - Before writing new code
   ```bash
   ./refactor-skill.sh find-uses SymbolName
   # Shows all uses of a symbol
   ```

4. **Find dead code** - Clean up before adding new code
   ```bash
   ./refactor-skill.sh find-unused ./internal
   # Identifies potentially unused symbols
   ```

### When Claude Should Use Analysis Tools

✅ **Use analysis tools when**:
- Starting a new implementation phase
- Before refactoring existing code
- When designing interfaces or abstractions
- To understand what code already exists
- To verify assumptions about code structure

❌ **Skip analysis if**:
- Writing simple standalone code (test cases)
- Feature is entirely new with no existing patterns
- Task is to write documentation, not code

### Example: Phase 4 Implementation

Before implementing `find-implementations`:
```bash
# 1. Find how interfaces are currently analyzed
./refactor-skill.sh find-callers "collectDefinitions"

# 2. Check for duplicate interface handling code
./refactor-skill.sh analyze-dir ./analyzer

# 3. Find uses of existing interface types
./refactor-skill.sh find-uses "InterfaceType"
```

This helps Claude:
- Reuse patterns from Phase 2-3
- Avoid duplicating analysis logic
- Understand how interfaces are currently represented
- Build on existing infrastructure

### Analysis Tool Commands

```bash
# Find all uses of a symbol across the codebase
./refactor-skill.sh find-uses <symbol>

# Find all functions that call a target
./refactor-skill.sh find-callers <function>

# Find potentially unused symbols
./refactor-skill.sh find-unused <directory>

# Analyze directory for duplicate patterns
./refactor-skill.sh analyze-dir <directory>

# Show implementations of an interface
./refactor-skill.sh show-interface <type>

# Check safety of proposed changes
./refactor-skill.sh check-safety <action>

# Show help
./refactor-skill.sh help
```

## Notes for Future Work

- The orchestrator is the primary user-facing feature for batch operations; individual commands are useful for one-off analysis
- Diff analysis (`analyze-diff`) is valuable for understanding what changed and generating corresponding refactoring plans
- The semantic targeting system is the key innovation—it enables refactoring plans to remain valid across code evolution
- Plans can include conditions to ensure operations only run when appropriate, increasing safety
