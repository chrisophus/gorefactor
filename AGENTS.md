# GoRefactor Agent Rules

This file provides guidance for coding agents working with the gorefactor repository.

## Quick Start

**GoRefactor is a harness for Go refactoring.** When working on `.go` files:

1. ✅ **Prefer gorefactor commands** for mutations (creates, moves, renames, extracts, deletes)
2. ✅ **Use gorefactor analysis commands** for understanding code (find-callers, find-uses, recommend)
3. ❌ **Avoid Write/Edit on .go files** — use gorefactor instead for safety and token efficiency

## Default Rule: Prefer GoRefactor for Go Files

When modifying any `.go` file:
- Use a gorefactor command instead of `Write` or `Edit`
- GoRefactor parses AST, infers types, runs goimports, validates syntax
- Failure mode: "command rejects malformed code" instead of "file silently breaks"
- Token cost is typically 80-95% cheaper than LLM-generated code

### Common Edits → GoRefactor Commands

| Want to... | Use |
|-----------|-----|
| Create a new .go file | `gorefactor create <path> -` (reads stdin) |
| Add a function/type | `gorefactor insert <file> at-end -` |
| Add after another function | `gorefactor insert <file> after:FuncName -` |
| Add inside a function | `gorefactor insert <file> inside:FuncName -` |
| Move function/method to new file | `gorefactor move <src> <Func> <dest>` |
| Replace a statement | `gorefactor replace <file> <Func> <old> <new>` |
| Replace text in function body | `gorefactor replace-text <file> <Func> <old> <new>` |
| Replace whole function body | `gorefactor replace-body <file> <Func> -` |
| Delete a function/method | `gorefactor delete <file> <Func> --safe` |
| Inline a function | `gorefactor inline <file> <Func>` |
| Rename unexported symbol | `gorefactor rename <file> <old> <new>` |
| Add struct field | `gorefactor add-field <file> <Struct> "<Name> <Type>"` |
| Change function signature | `gorefactor change-signature <file> <Func> --add-param "n T"` |
| Extract a code block | `gorefactor extract <file> <startLine> <endLine> <methodName>` |

### When Edit/Write IS OK for Go Files

Only use `Edit`/`Write` on `.go` files when:
- No gorefactor command applies (rare)
- You've documented why in a comment

**Acceptable non-Go file edits:**
- Markdown (README.md, docs, guides)
- YAML (CI config, manifests)
- JSON (go.mod, plans, configs)
- Makefiles and shell scripts
- Git operations (commits, branches)

## Token Efficiency: Decision Matrix

**High savings (use GoRefactor):**
- Move method to new file: 99% savings (target by name, instant execution)
- Rename variable: 99% savings (semantic find-replace)
- Delete unused function: 99% savings (just needs location)
- Extract method: 95% savings (identify block, tool infers signature)
- Batch refactoring (5 files): 80% savings (one plan, execute in parallel)

**Moderate savings (use GoRefactor analysis):**
- Find all callers: 90% savings (semantic analysis, no code generation)
- Find implementations: 90% savings (graph traversal)
- Recommend extractions: 85% savings (complexity analysis)

**Low/negative (use Claude):**
- Rewrite algorithm: Use Claude (requires reasoning about intent)
- Fix bug: Use Claude (needs semantic understanding of what's broken)
- Add feature: Use Claude (logic-level changes)
- Error handling: Use Claude (domain-specific knowledge)

## Workflow: Maximizing Token Efficiency

1. **Analyze first** (free): Run `gorefactor recommend`, `analyze-diff`, `find-callers`
2. **LLM reviews** (~50 tokens): Read JSON output, decide which operations
3. **Create batch plan** (~100 tokens): Combine multiple edits into one JSON plan
4. **Execute** (instant): `gorefactor orchestrate plan.json`
5. **Gate** (instant): `gorefactor doctor` (lint + build + test)
6. **Verify** (~100 tokens): Review results

**Total for 5 refactoring changes: ~250 tokens** (vs 1000+ if doing manually)

## Commands by Category

### Analysis Commands (Read-Only, Free)

Use these to understand code structure before refactoring:

```bash
gorefactor parse <file>                    # Parse Go file → JSON
gorefactor inspect <file>                  # One-page summary
gorefactor recommend <file>                # Extract candidates
gorefactor find-callers <Func>             # All callers
gorefactor find-uses <Symbol>              # All uses
gorefactor find-implementations <Iface>    # All implementing types
gorefactor callgraph <Func>                # Call tree
gorefactor analyze-dir <path>              # Duplication detection
gorefactor lint <path>                     # Code quality issues
```

### Direct Mutation Commands (Use These Directly)

When you know exactly what to change:

```bash
gorefactor create <path> -                 # New file (reads stdin)
gorefactor insert <file> <location> -      # Insert code
gorefactor replace <file> <Func> <old> <new>  # Replace statement
gorefactor replace-text <file> <Func> <old> <new>  # Replace text
gorefactor replace-body <file> <Func> -    # Replace body
gorefactor delete <file> <Func> --safe     # Delete with safety check
gorefactor move <src> <Func> <dest>        # Move between files
gorefactor rename <file> <old> <new>       # Rename symbol
gorefactor inline <file> <Func>            # Inline function
gorefactor extract <file> <start> <end> <name>  # Extract block
```

### Batch Orchestration (Use for Complex Refactors)

When you need multiple coordinated changes:

```bash
gorefactor orchestrate <plan.json>         # Execute JSON plan
gorefactor exec                            # Single operation from stdin
gorefactor txn                              # Batch with undo
gorefactor undo                             # Rollback last operation
```

### Quality Gates

Always run before committing:

```bash
gorefactor doctor                          # Full gate: lint + build + test
gorefactor lint . --fix                    # Autofix safe issues
gorefactor test-affected                   # Tests for changed code
```

## Receiver-Method Syntax

Methods are referenced as `Receiver:Method` everywhere:
- `CodeInserter:InsertCode` (pointer receiver, no `*` needed)
- `Parser:Parse`
- `Orchestrator:Execute`

## Stdin Convention

Commands accepting content use `-` to read from stdin (Unix convention):

```bash
gorefactor create src/helper.go - << 'EOF'
func Helper() string { return "help" }
EOF

# Or pipe from tools
echo "func X() {}" | gorefactor create src/new.go -
```

## Project Structure

- **Parser** (`parser/`): AST analysis, JSON output
- **Analyzer** (`analyzer/`): Complexity analysis, recommendations
- **Orchestrator** (`orchestrator/`): Batch refactoring engine
- **CLI** (`cmd/gorefactor/`): Command registration and implementation
- **Agent** (`cmd/gorefactor-agent/`): LLM harness for interactive refactoring

See [CLAUDE.md](CLAUDE.md) for detailed architecture.

## Skill

When in pi interactive mode, use `/skill:gorefactor` to load the full GoRefactor skill with detailed examples and reference tables.

```
/skill:gorefactor
```

## Examples

### Extract a method (token-efficient)

```bash
# 1. Identify candidates
gorefactor recommend ./myfile.go --short

# 2. Extract (no LLM code generation needed)
gorefactor extract ./myfile.go 45 60 validateInput

# 3. Verify
gorefactor doctor
```

### Batch move related functions to new file

```bash
# Create a plan to move 3 helpers
cat > move-helpers.json << 'EOF'
{
  "version": "1.0",
  "name": "consolidate-helpers",
  "operations": [
    {"type": "move_method", "target": {"functionName": "validateEmail"}, "newFile": "validators.go"},
    {"type": "move_method", "target": {"functionName": "validatePhone"}, "newFile": "validators.go"},
    {"type": "move_method", "target": {"functionName": "formatPhone"}, "newFile": "validators.go"}
  ]
}
EOF

# Execute all three moves atomically
gorefactor orchestrate move-helpers.json
```

### Find what calls a function before refactoring

```bash
gorefactor find-callers ParseRequest --json
# Output shows all 8 places that call ParseRequest
# Safe to refactor knowing the impact
```

## Testing

After any refactoring:

```bash
# Quick check
go test ./...

# Coverage
go test ./... -cover

# Via gorefactor
gorefactor doctor
```

## Further Reading

- [README.md](README.md) — User guide and features
- [CLAUDE.md](CLAUDE.md) — Detailed architecture and advanced usage
- [ORCHESTRATION_SYSTEM.md](ORCHESTRATION_SYSTEM.md) — JSON plan specification
