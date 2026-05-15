# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

GoRefactor is a command-line tool for analyzing and refactoring Go code. It focuses on method extraction and intelligent code analysis through a sophisticated JSON-based orchestration system. The tool provides both interactive commands and batch refactoring capabilities through resilient, semantic-based code targeting.

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

Main commands in `main.go`:

- `parse`: Parse a Go file → outputs JSON structure
- `list-functions`: Extract function/method list from a file
- `recommend`: Analyze file and recommend extraction candidates
- `extract`: Extract a specific code block into a method
- `orchestrate`: Execute JSON refactoring plan (primary batch operation)
- `generate-templates`: Create example JSON plan templates
- `analyze-diff`: Generate refactoring plan from git diff

## Development Commands

### Building

```bash
# Build the binary
go build -o gorefactor main.go

# Build with specific output location
go build -o ./bin/gorefactor main.go
```

### Testing

```bash
# Run all tests
go test ./...

# Run tests for specific package
go test ./analyzer
go test ./parser
go test ./extractor
go test ./orchestrator

# Run tests with verbose output
go test -v ./...

# Run a specific test
go test -v -run TestAnalyzeBlock ./analyzer
```

### Running Commands Locally

```bash
# After building, run commands like:
./gorefactor parse path/to/file.go
./gorefactor analyze-diff diff.patch
./gorefactor orchestrate plan.json
```

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

## Notes for Future Work

- The orchestrator is the primary user-facing feature for batch operations; individual commands are useful for one-off analysis
- Diff analysis (`analyze-diff`) is valuable for understanding what changed and generating corresponding refactoring plans
- The semantic targeting system is the key innovation—it enables refactoring plans to remain valid across code evolution
- Plans can include conditions to ensure operations only run when appropriate, increasing safety
