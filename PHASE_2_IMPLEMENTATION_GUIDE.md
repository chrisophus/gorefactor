# Phase 2: Extract Command Enhancement - Implementation Guide

**Phase**: 2 of 4  
**Duration**: 4-6 hours  
**Objective**: Integrate DetailedError into extract command for autonomous LLM error recovery

---

## Overview

Phase 1 created the infrastructure. Phase 2 will implement detailed error handling in the `extract` command so that:
- When extraction fails, LLM gets structured feedback
- LLM can understand what went wrong
- LLM can attempt recovery automatically

---

## Files to Modify

### 1. `cmd/gorefactor/cmd_extract_extract.go`

**Current location**: Lines where errors are returned  
**What to do**: Replace simple errors with DetailedError builders

#### Change 1: Return Statement Check

**Current code (line ~55)**:
```go
if containsReturn(blockStmts) {
    return m.fail(fmt.Errorf("block contains a return statement; v1 extract does not handle this"))
}
```

**New code**:
```go
if containsReturn(blockStmts) {
    returnLines := findReturnLines(fset, blockStmts)
    err := ExampleReturnStatementError(file, startLine, endLine, returnLines)
    return m.fail(err)
}
```

**Add helper function** (at end of file):
```go
// findReturnLines returns line numbers of all return statements in block
func findReturnLines(fset *token.FileSet, stmts []ast.Stmt) []int {
    var lines []int
    for _, stmt := range stmts {
        ast.Inspect(stmt, func(n ast.Node) bool {
            if ret, ok := n.(*ast.ReturnStmt); ok {
                lines = append(lines, fset.Position(ret.Pos()).Line)
            }
            return true
        })
    }
    return lines
}
```

**Test case to add**:
```go
// In cmd_extract_extract_test.go
func TestExtractWithReturnStatement(t *testing.T) {
    // Try extracting code with return statement
    // Verify errorDetails.code == "RETURN_IN_BLOCK"
    // Verify 3+ suggestions present
    // Verify JSON output is valid
}
```

---

#### Change 2: Invalid Range Check

**Current code** (doesn't exist, need to add):
```go
// After parsing startLine/endLine
if startLine < 1 || endLine < startLine || endLine > maxLines {
    return m.fail(usageErrorf("invalid range: %d-%d", startLine, endLine))
}
```

**New code**:
```go
if startLine < 1 || endLine < startLine || endLine > maxLines {
    err := NewDetailedError(ErrInvalidRange,
        fmt.Sprintf("Invalid extraction range: %d-%d", startLine, endLine)).
        WithContext(file, startLine, endLine, "Line numbers out of bounds").
        WithRootCause(fmt.Sprintf("File has %d lines, range %d-%d invalid", maxLines, startLine, endLine)).
        WithSuggestion("use_valid_range",
            fmt.Sprintf("Use range within 1-%d", maxLines),
            0.99).
        WithDetail("filePath", file).
        WithDetail("totalLines", maxLines).
        WithDetail("requestedStart", startLine).
        WithDetail("requestedEnd", endLine)
    return m.fail(err)
}
```

---

#### Change 3: Type Analysis Error (After `analyzeBlockTypes`)

**Current code** (line ~70):
```go
params, returns, err := analyzeBlockTypes(pkg, fileAST, enclosing, blockStmts)
if err != nil {
    return m.fail(err)
}
```

**New code**:
```go
params, returns, err := analyzeBlockTypes(pkg, fileAST, enclosing, blockStmts)
if err != nil {
    // Check if it's a variable scope issue
    if strings.Contains(err.Error(), "undefined") {
        // Extract variable names from error
        undefinedVars := extractUndefinedVars(err.Error())
        // Create detailed error
        err := NewDetailedError(ErrVariableOutOfScope,
            fmt.Sprintf("Cannot extract: undefined variables: %v", undefinedVars)).
            WithContext(file, startLine, endLine, "Variables used but not defined in range").
            WithSuggestion("add_parameter",
                "Add undefined variables as parameters",
                0.95).
            WithSuggestion("expand_range",
                "Include variable definitions in extraction range",
                0.85).
            WithDetail("undefinedVariables", undefinedVars)
        return m.fail(err)
    }
    
    // For other errors, wrap with context
    err := NewDetailedError(ErrGeneric, err.Error()).
        WithContext(file, startLine, endLine, "Type analysis failed").
        WithRootCause("Could not determine types of variables in extraction block")
    return m.fail(err)
}
```

**Add helper function**:
```go
// extractUndefinedVars parses error message to find undefined variable names
func extractUndefinedVars(errMsg string) []string {
    // Simple parsing: look for patterns like "undefined: varName"
    // More robust: use regex
    // For now, return empty slice (can improve later)
    return []string{}
}
```

---

### 2. `cmd/gorefactor/cmd_extract_extract_test.go`

**Add test cases**:

```go
// Add to existing test file

func TestExtractInvalidRange(t *testing.T) {
    // Create test file
    src := `package main
func Foo() {
    x := 1
}`
    
    // Try invalid range
    result := runExtractTest(src, 1, 100, "bar") // 100 is beyond file
    
    if result.Success {
        t.Fatal("Expected extraction to fail")
    }
    
    if result.ErrorDetails == nil {
        t.Fatal("Expected ErrorDetails to be set")
    }
    
    if result.ErrorDetails.Code != "INVALID_RANGE" {
        t.Errorf("Expected INVALID_RANGE, got %s", result.ErrorDetails.Code)
    }
    
    if len(result.ErrorDetails.Suggestions) == 0 {
        t.Fatal("Expected suggestions")
    }
}

func TestExtractWithUndefinedVariable(t *testing.T) {
    // Create test file with undefined variable
    src := `package main
func ProcessOrder(order Order) {
    // Lines 3-5 use 'config' which is not defined
    logger.Info(config.Value)
    fmt.Println(config.Name)
}`
    
    // Try extracting lines that use undefined 'config'
    result := runExtractTest(src, 3, 5, "validateOrder")
    
    if result.Success {
        t.Fatal("Expected extraction to fail due to undefined variable")
    }
    
    if result.ErrorDetails.Code != "VARIABLE_OUT_OF_SCOPE" {
        t.Errorf("Expected VARIABLE_OUT_OF_SCOPE, got %s", result.ErrorDetails.Code)
    }
    
    // Verify suggestions
    if len(result.ErrorDetails.Suggestions) < 2 {
        t.Fatal("Expected at least 2 suggestions")
    }
    
    // First suggestion should be add_parameter (0.95 likelihood)
    if result.ErrorDetails.Suggestions[0].Approach != "add_parameter" {
        t.Errorf("Expected first suggestion to be add_parameter, got %s",
            result.ErrorDetails.Suggestions[0].Approach)
    }
    
    // Verify command is present
    if result.ErrorDetails.Suggestions[0].Command == "" {
        t.Fatal("Expected command in suggestion")
    }
}

func TestExtractWithReturnStatementDetailed(t *testing.T) {
    src := `package main
func ProcessOrder() error {
    if err := validate(); err != nil {
        return err
    }
}`
    
    // Try extracting lines with return
    result := runExtractTest(src, 2, 4, "checkValidation")
    
    if result.Success {
        t.Fatal("Expected extraction to fail due to return statement")
    }
    
    if result.ErrorDetails.Code != "RETURN_IN_BLOCK" {
        t.Errorf("Expected RETURN_IN_BLOCK, got %s", result.ErrorDetails.Code)
    }
    
    // Should have multiple suggestions
    if len(result.ErrorDetails.Suggestions) < 2 {
        t.Fatal("Expected multiple suggestions")
    }
    
    // Verify return line is recorded
    if returnLines, ok := result.ErrorDetails.Details["returnLines"].([]int); !ok || len(returnLines) == 0 {
        t.Fatal("Expected return line numbers in details")
    }
}
```

---

## Implementation Steps

### Step 1: Add Helper Functions (30 min)

In `cmd_extract_extract.go`, add:
```go
// findReturnLines: returns []int of return statement line numbers
func findReturnLines(fset *token.FileSet, stmts []ast.Stmt) []int { ... }

// extractUndefinedVars: parses error message to find undefined variables
func extractUndefinedVars(errMsg string) []string { ... }

// findVariableDefinitionLine: returns line number where variable is defined
func findVariableDefinitionLine(fset *token.FileSet, node ast.Node, varName string) int { ... }
```

### Step 2: Modify Error Handling (1-2 hours)

Replace 3-4 error returns with DetailedError:
1. Return statement check
2. Invalid range check
3. Type analysis failure
4. Function not found check

### Step 3: Add Test Cases (1-2 hours)

Create test cases that verify:
- ✅ ErrorDetails field is populated
- ✅ Error code is correct
- ✅ Suggestions are present and sorted
- ✅ JSON output is valid
- ✅ Command suggestions are executable

### Step 4: Integration Test (30 min)

```bash
# Build
go build -o gorefactor ./cmd/gorefactor

# Test with --json flag
./gorefactor extract test_file.go 10 20 newFunc --json

# Should see errorDetails in JSON output
```

### Step 5: Pi/LLM Testing (1 hour)

Manually test with pi:
```
User: "Extract lines 10-20 from file.go"

Pi calls: gorefactor extract file.go 10 20 extractedFunc --json
Pi receives: JSON with errorDetails if extraction failed
Pi parses: suggestions[0] for best recovery action
Pi executes: suggested command
Pi retries: original extraction
Result: Success or better error message
```

---

## What LLM Should See

### Success Case
```json
{
  "success": true,
  "operation": "extract",
  "detail": "Extracted validateOrder (params=2, returns=1)",
  "filesChanged": ["order.go"]
}
```

### Failure Case: Undefined Variable
```json
{
  "success": false,
  "operation": "extract",
  "errorDetails": {
    "code": "VARIABLE_OUT_OF_SCOPE",
    "message": "Cannot extract: undefined variables: [config]",
    "suggestions": [
      {
        "approach": "add_parameter",
        "description": "Add config as parameter to extracted function",
        "command": "gorefactor change-signature order.go validateOrder --add-param \"config Config\"",
        "likelihood": 0.95
      }
    ]
  }
}
```

**LLM can now**:
1. See the error code
2. Read the root cause
3. Parse suggestions in priority order
4. Execute the suggested command
5. Retry without human intervention

---

## Verification Checklist

- [ ] All new errors use DetailedError
- [ ] All suggestions include likelihood (0.0-1.0)
- [ ] Suggestions are sorted by likelihood (descending)
- [ ] Commands in suggestions are executable
- [ ] Test coverage > 80%
- [ ] `go build` succeeds
- [ ] `go test ./cmd/gorefactor` all pass
- [ ] `./gorefactor doctor` passes
- [ ] JSON output valid for all error cases

---

## Common Patterns to Follow

### Pattern 1: Simple Error with One Suggestion
```go
err := NewDetailedError(ErrCode, "message").
    WithContext(file, start, end, "description").
    WithRootCause("why it failed").
    WithSuggestion("approach", "description", 0.95)
return m.fail(err)
```

### Pattern 2: Error with Multiple Suggestions (Sorted)
```go
err := NewDetailedError(ErrCode, "message").
    WithContext(file, start, end, "description").
    WithSuggestion("best", "best option", 0.95).
    WithSuggestion("good", "good option", 0.70).
    WithSuggestion("fallback", "fallback", 0.30)
return m.fail(err)
```

### Pattern 3: Error with Command Suggestions
```go
err := NewDetailedError(ErrCode, "message").
    WithSuggestionCommand("approach",
        "Try this command",
        "gorefactor <operation> <args>",
        0.90)
return m.fail(err)
```

---

## Success Criteria

| Criterion | Weight | Done |
|-----------|--------|------|
| All extract errors return DetailedError | 30% | [ ] |
| All error codes match ErrXxx constants | 20% | [ ] |
| Suggestions sorted by likelihood | 20% | [ ] |
| Test coverage > 80% | 15% | [ ] |
| JSON output valid for all cases | 15% | [ ] |

**Pass**: 5/5 criteria met

---

## Next: Phase 3

After Phase 2 succeeds:
- Implement similar error handling for:
  - Move command
  - Insert command
  - Delete command
  - Replace command

**Estimated duration**: 2-3 hours for each command

---

## Questions?

If you get stuck:
1. Check `error_context.go` for available builders
2. Review test cases in `error_context_test.go`
3. Check `ExampleVariableOutOfScopeError()` for reference implementation
4. Run tests: `go test ./cmd/gorefactor/error_context_test.go -v`

---

**Ready to implement Phase 2?** Start with Step 1: Add Helper Functions.

Estimated time: 4-6 hours  
Estimated value: ~5000+ tokens saved when LLM recovers from errors autonomously
