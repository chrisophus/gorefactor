# GoRefactor Skill

A Claude Code skill that leverages the GoRefactor tool for intelligent, automated code refactoring. This skill makes refactoring decisions more efficiently than manual code editing by analyzing code complexity and applying safe transformations.

**Prefer the main CLI** for new work — see [README.md](README.md). This document covers the legacy `skill-refactor` binary and wrapper scripts.

## Quick Start (recommended path)

The main `gorefactor` binary is now self-sufficient. Use these CLI commands instead of `Write`/`Edit` for any `.go` file change:

```bash
# Read-only sensors
./gorefactor lint .                          # structural issues (file-size, duplicates, untested-package, extract-candidates)
./gorefactor inspect path/to/file.go         # one-page human-readable file summary
./gorefactor find-callers MyFunc             # who calls X (with `Receiver:Method` for methods)
./gorefactor find-uses MySymbol              # where X is referenced
./gorefactor find-implementations MyIface    # types satisfying an interface

# Mutations (never use Write/Edit on .go files; use these)
./gorefactor create path/to/new.go -         # create file (- = read stdin)
./gorefactor insert file.go after:Func -     # insert code at semantic location
./gorefactor replace file.go Func "<old>" "<new>"          # AST replace within func body
./gorefactor replace-text file.go Func "<old>" "<new>"     # literal text replace within func body
./gorefactor delete file.go Func             # delete declaration
./gorefactor rename file.go old new          # rename unexported symbol package-wide
./gorefactor move src.go Func dest.go        # move declaration between files
./gorefactor split file.go                   # auto-split file if over size limit

# Aggregate
./gorefactor lint . --fix                    # autofix file-size violations
./gorefactor doctor                          # lint + build + test gate; non-zero on failure
```

`Receiver:Method` selects a method (e.g. `CodeInserter:InsertCode`). `-` as the final arg reads stdin (for multi-line code, this avoids quoting).

The legacy `skill-refactor` binary documented below predates these commands and is useful mainly for the priority-ranked auto-extraction workflow. For everything else, prefer the gorefactor CLI.

## When to Use This Skill

Use the GoRefactor skill when:

- **Simplifying complex functions**: Function is hard to read and has multiple logical blocks that could be extracted
- **Reducing cyclomatic complexity**: Code has many nested conditions, loops, or error handling paths
- **Improving testability**: Large functions would benefit from breaking into smaller, testable methods
- **Analyzing code structure**: Need to understand complexity and recommend specific improvements
- **Processing diffs**: Need to generate refactoring plans from changes

Avoid when:

- Making semantic or behavioral changes (use LLM edits for logic changes)
- Renaming things for clarity (LLM is better at naming in context)
- Restructuring code architecturally (requires understanding business logic)
- Working with tests (refactoring test code is riskier)

## Installation

The skill binaries are built automatically:

```bash
# Bash wrapper (higher-level commands)
./refactor-skill.sh

# Go binary (structured JSON output for Claude Code)
./skill-refactor
```

Ensure `gorefactor` binary is built first:

```bash
go build -o gorefactor ./cmd/gorefactor
```

## Commands

### Go Binary Interface (`skill-refactor`)

Best for integration with Claude Code due to structured JSON output.

#### analyze

Analyze a Go file and recommend extraction candidates.

```bash
./skill-refactor analyze path/to/file.go
```

**Output**: JSON with extraction recommendations sorted by priority (1-10), including:
- Line ranges
- Complexity metrics
- Variable dependencies
- Suggested method names
- Extractability assessment

**Example**:

```json
{
  "success": true,
  "operation": "analyze",
  "file": "service.go",
  "recommendations": [
    {
      "startLine": 45,
      "endLine": 62,
      "complexity": 6,
      "statementCount": 12,
      "readVars": ["user", "config"],
      "writeVars": ["result"],
      "extractable": true,
      "priority": 8,
      "suggestedName": "calculateResult"
    }
  ],
  "message": "Found 3 extraction candidates"
}
```

#### refactor

Automatically apply safe refactorings to a file.

```bash
./skill-refactor refactor path/to/file.go [max-extractions]
```

**Options**:
- `max-extractions`: Maximum number of refactorings to apply (default: 3)

**Output**: JSON describing applied changes

**Note**: Modifies the file in-place. The skill applies highest-priority extractions based on:
- Complexity (sweet spot: 3-10)
- Clear inputs/outputs (has read and write variables)
- Maintainability (statement count <= 20)

#### extract

Extract a specific code block into a method.

```bash
./skill-refactor extract file.go <startLine> <endLine> <methodName>
```

**Parameters**:
- `startLine`: First line of code block (1-indexed)
- `endLine`: Last line of code block (inclusive)
- `methodName`: Name for the new method

**Example**:

```bash
./skill-refactor extract service.go 45 62 validateUserData
```

#### suggest

Analyze file and suggest refactorings without applying them (same as `analyze`).

```bash
./skill-refactor suggest path/to/file.go
```

### Bash Wrapper (`refactor-skill.sh`)

User-friendly interface with colored output.

#### analyze

```bash
./refactor-skill.sh analyze path/to/file.go
```

Shows extraction candidates with readability formatting.

#### extract-best

Automatically finds and extracts the highest-priority candidate.

```bash
./refactor-skill.sh extract-best path/to/file.go
```

#### extract

```bash
./refactor-skill.sh extract path/to/file.go <line1> <line2> <method-name>
```

#### simplify

Apply multiple high-impact refactorings to a file.

```bash
./refactor-skill.sh simplify path/to/file.go [max-extractions]
```

#### plan-diff

Generate a refactoring plan from a git diff.

```bash
./refactor-skill.sh plan-diff diff.patch
```

#### apply-plan

Execute a pre-built refactoring plan.

```bash
./refactor-skill.sh apply-plan refactoring-plan.json [output.json]
```

## How It Works

### Priority Calculation

The skill calculates priority (1-10) for each extraction based on:

| Factor | Priority Change |
|--------|-----------------|
| Extractable block | Required (0 = not extractable) |
| Complexity 3-10 | +2 |
| Has both inputs and outputs | +1 |
| ≤ 20 statements | +1 |
| Complexity < 2 | -3 |

Higher priority blocks are extracted first.

### Method Naming Strategy

The skill suggests method names based on code characteristics:

- **If writes to variables**: `calculate<VarName>` (e.g., `calculateTotal`)
- **If reads from variables**: `validate<VarName>` (e.g., `validateInput`)
- **High complexity (>7)**: `refactor`
- **Default**: `extract`

### Complexity Metrics

The analysis evaluates:

- **Cyclomatic Complexity**: Number of if/for/switch statements
- **Statement Count**: Total executable statements
- **Variable Dependencies**: Number of inputs/outputs
- **Error Handling Paths**: Error checks and returns
- **Nesting Depth**: Depth of control structures

## Example Workflow

### Scenario: Simplify a Complex Service Function

**File**: `service.go` with a 40-line `ProcessOrder` function

**Step 1: Analyze**

```bash
./skill-refactor analyze service.go
```

Output shows 4 extraction candidates with complexity 5-8.

**Step 2: Review Suggestions**

Highest priority: lines 20-32 (validate payment block, complexity 6)

**Step 3: Apply Refactoring**

```bash
./skill-refactor refactor service.go
```

Applies top 3 refactorings:
1. Lines 20-32 → `validatePayment`
2. Lines 35-40 → `calculateTotal`
3. Lines 12-18 → `loadConfig`

**Result**: `ProcessOrder` now has 4-5 semantic calls instead of 40 lines, each extracted method handles one concern.

## Integration with Claude Code

When working with the skill from Claude Code:

1. **Analyze first**:
   ```bash
   ./skill-refactor analyze path/to/file.go
   ```
   Review the JSON recommendations to understand opportunities.

2. **Apply selectively**:
   ```bash
   ./skill-refactor refactor path/to/file.go 2
   ```
   Apply 2 highest-priority refactorings.

3. **Verify manually**:
   Check the refactored file to ensure the extractions make sense in context.

4. **Use for specific blocks**:
   ```bash
   ./skill-refactor extract file.go <start> <end> <method>
   ```
   When you want full control over which block to extract.

## Output Format

### JSON Output Structure

```json
{
  "success": true,
  "operation": "refactor|analyze|extract",
  "file": "path/to/file.go",
  "changes": [
    {
      "type": "extract_method",
      "startLine": 45,
      "endLine": 62,
      "methodName": "validateUser"
    }
  ],
  "recommendations": [
    {
      "startLine": 45,
      "endLine": 62,
      "complexity": 6,
      "statementCount": 12,
      "readVars": ["user"],
      "writeVars": ["result"],
      "extractable": true,
      "priority": 8,
      "suggestedName": "calculateResult"
    }
  ],
  "message": "Refactoring completed successfully",
  "details": {}
}
```

## Limitations and Considerations

### What It Can't Do

- **Rename for meaning**: Generates names like `calculateResult`, not contextual names like `computeDiscountedPrice`
- **Semantic refactoring**: Won't change logic or behavior
- **Cross-file refactoring**: Works on single files only
- **Architectural changes**: Can't reorganize class hierarchies or interfaces

### Edge Cases

- **Very short functions** (< 3 statements): Not recommended for extraction
- **Very long functions** (> 100 statements): May need manual split first
- **Global state**: Extractions involving package-level variables may need manual adjustment
- **Defer/panic/recover**: Limited support for complex control flow

## Advanced Usage

### Creating Custom Plans

Generate a template:

```bash
./gorefactor generate-templates ./templates
```

Edit `templates/extract_method_template.json` to create a custom plan:

```json
{
  "version": "1.0",
  "name": "custom_refactoring",
  "operations": [
    {
      "type": "extract_method",
      "file": "service.go",
      "target": {
        "functionName": "ProcessOrder",
        "codePattern": "if err != nil"
      },
      "parameters": {
        "methodName": "handleError"
      },
      "fallback": {
        "type": "skip"
      }
    }
  ]
}
```

Apply with:

```bash
./refactor-skill.sh apply-plan custom_refactoring.json results.json
```

## Exit Codes

- `0`: Success
- `1`: Error (check stderr for details)

## Troubleshooting

### "Binary not found"

```bash
go build -o gorefactor ./cmd/gorefactor
go build -o skill-refactor ./skill/skill.go
```

### "No extractable candidates found"

File may already be well-refactored, or all blocks fall outside complexity bounds. Try with relaxed filters:

```bash
./gorefactor recommend file.go --min-complexity 1 --min-statements 2
```

### "Extraction failed"

Check that:
- Line numbers are correct (1-indexed)
- Code at those lines is syntactically valid
- Method name is a valid Go identifier

## See Also

- `CLAUDE.md`: Main codebase documentation
- `ORCHESTRATION_SYSTEM.md`: Detailed refactoring plan system
- `README.md`: Tool overview and usage examples
