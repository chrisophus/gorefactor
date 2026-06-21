---
name: gorefactor
description: GoRefactor command-line tool for analyzing and refactoring Go code. Use for method extraction, code analysis, mutations, and batch refactoring through semantic-based targeting. Preferred over Write/Edit for all .go file changes.
---

# GoRefactor Skill

GoRefactor is a token-efficient harness for Go refactoring. It parses the AST, infers types, runs goimports, and refuses to write malformed code—the failure mode is "command rejects the change" rather than "file silently breaks."

**Key principle:** Prefer `gorefactor` commands over `Write` or `Edit` for **all `.go` file mutations**. This is cheaper in tokens and safer.

## When to Use GoRefactor

✅ **Use GoRefactor for:**
- Method/function extraction (identify block, tool infers parameters/returns)
- Moving functions/methods between files (semantic targeting by name)
- Renaming unexported symbols (find-and-replace across package)
- Deleting code (just needs location)
- Inserting code at known locations
- Batch refactoring via orchestration plans
- Code analysis (find callers, uses, implementations)

❌ **Use Claude for:**
- Rewriting algorithms (requires semantic understanding)
- Bug fixes (needs to understand intent)
- New features (logic-level changes)
- Renaming for clarity (semantic judgment)
- Error handling changes (domain knowledge)

## Quick Reference

| Task | Command |
|------|---------|
| Create new .go file | `gorefactor create <path> -` |
| Add function to file | `gorefactor insert <file> at-end -` |
| Add function after another | `gorefactor insert <file> after:Func -` |
| Add helper inside function | `gorefactor insert <file> inside:Func -` |
| Move function/method | `gorefactor move <src> <Func> <dest>` |
| Replace statement | `gorefactor replace <file> <Func> <old> <new>` |
| Replace text in function | `gorefactor replace-text <file> <Func> <old> <new>` |
| Replace whole function body | `gorefactor replace-body <file> <Func> -` |
| Delete function | `gorefactor delete <file> <Func> --safe` |
| Inline simple function | `gorefactor inline <file> <Func>` |
| Rename unexported symbol | `gorefactor rename <file> <old> <new>` |
| Add struct field | `gorefactor add-field <file> <Struct> "<Name> <Type>"` |
| Change function signature | `gorefactor change-signature <file> <Func> --add-param "n T"` |
| Flip method receiver | `gorefactor change-receiver <file> <Type:Method> --pointer` |
| Extract method | `gorefactor extract <file> <startLine> <endLine> <methodName>` |
| Find extraction candidates | `gorefactor recommend <file>` |
| Analyze code structure | `gorefactor parse <file>` |
| Find callers | `gorefactor find-callers <Func\|Receiver:Method>` |
| Find uses | `gorefactor find-uses <Symbol>` |
| Find implementations | `gorefactor find-implementations <Interface>` |
| Lint code quality | `gorefactor lint .` |
| Build + test gate | `gorefactor doctor` |

## Syntax Notes

- **Method references:** Use `Receiver:Method` (e.g., `CodeInserter:InsertCode`, pointer receivers don't need `*`)
- **Stdin convention:** Commands accepting content use `-` to read stdin: `gorefactor create path/file.go -`
- **Receiver-first syntax:** When specifying methods, use `TypeName:MethodName` format

## Token Efficiency

Using gorefactor saves tokens compared to LLM code generation:

| Operation | Savings |
|-----------|---------|
| Move 200-line function | 99.5% (plan in 100 tokens, execute instantly) |
| Extract method | 95%+ (identify block, tool infers signature) |
| Apply pattern to 5 files | 80%+ (one plan, five executions) |
| Rename function | 99% (semantic targeting, instant execution) |

## Workflow

1. **Analyze** (free): `gorefactor recommend`, `analyze-diff` → get JSON
2. **LLM reviews briefly** (~50 tokens): Scan output, decide operations
3. **Create plan** (~100 tokens): Batch multiple operations
4. **Execute** (instant): `gorefactor orchestrate plan.json`
5. **Verify** (~100 tokens): Check test output

**Total for 5 changes:** ~250 tokens (vs 1000+ doing it manually)

## Analysis Commands

Use these to understand code before refactoring:

```bash
# Find extraction candidates
gorefactor recommend ./file.go

# Analyze call tree
gorefactor callgraph FunctionName [--callers]

# Find all callers
gorefactor find-callers FunctionName

# Find all uses
gorefactor find-uses SymbolName

# Find implementations
gorefactor find-implementations InterfaceName

# Cross-file duplicate detection
gorefactor analyze-dir ./pkg

# Generate LLM context pack
gorefactor context SymbolName
```

## Orchestration Plans

For batch refactoring, create a JSON plan and execute it:

```json
{
  "version": "1.0",
  "name": "batch_refactor",
  "operations": [
    {
      "type": "move_method",
      "target": { "functionName": "Helper1" },
      "newFile": "helpers.go"
    },
    {
      "type": "extract_method",
      "target": { "functionName": "Process" },
      "parameters": { "startLine": 45, "endLine": 60, "newMethodName": "validate" }
    }
  ]
}
```

Then execute: `gorefactor orchestrate plan.json`

## When Edit/Write is OK

Use `Edit`/`Write` for:
- Non-Go files (Markdown, YAML, JSON, Makefile, go.mod)
- Git operations
- Multi-file new packages where stdin friction outweighs benefit

For **.go file mutations, always use gorefactor commands first.** Fall back to `Edit` only when no gorefactor command applies and document why.

## Final Gates

After refactoring:

```bash
# Full quality gate (lint + build + test)
gorefactor doctor

# Quick lint
gorefactor lint . --fix

# Test affected files
gorefactor test-affected
```

## Help & Discovery

```bash
gorefactor                    # Show all commands
gorefactor <command> -h       # Help for a command
gorefactor inspect <file>     # One-page summary
gorefactor skeleton <file>    # File shape (bodies elided)
gorefactor history            # Journal of operations
```
