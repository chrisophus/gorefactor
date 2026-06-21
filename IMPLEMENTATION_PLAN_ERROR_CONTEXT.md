# Implementation Plan: Better Error Context & Recovery Suggestions

**Objective**: Enable autonomous LLM refactoring loops by providing structured error responses with actionable recovery suggestions.

**Status**: Planning & Design  
**Target Duration**: 2-3 days  
**Impact**: ⭐⭐⭐⭐⭐ (Critical for pi)

---

## Design Overview

### 1. New Error Type: `DetailedError`

Create a structured error type that includes:
- Error message (what went wrong)
- Error code (machine-readable classification)
- Context (where it failed, affected code)
- Root causes (why it happened)
- Suggestions (how to fix it)

**File**: `cmd/gorefactor/error_context.go` (new)

```go
package main

import "encoding/json"

// ErrorCode categorizes errors for programmatic handling
type ErrorCode string

const (
	ErrVariableOutOfScope     ErrorCode = "VARIABLE_OUT_OF_SCOPE"
	ErrReturnStatementInBlock ErrorCode = "RETURN_IN_BLOCK"
	ErrInvalidRange           ErrorCode = "INVALID_RANGE"
	ErrFunctionNotFound       ErrorCode = "FUNCTION_NOT_FOUND"
	ErrImportCycle            ErrorCode = "IMPORT_CYCLE"
	ErrUnsafeExtraction       ErrorCode = "UNSAFE_EXTRACTION"
	ErrUnsafeMove             ErrorCode = "UNSAFE_MOVE"
	ErrTypeConflict           ErrorCode = "TYPE_CONFLICT"
	ErrMissingDependency      ErrorCode = "MISSING_DEPENDENCY"
	ErrParseError             ErrorCode = "PARSE_ERROR"
	ErrGeneric                ErrorCode = "GENERIC_ERROR"
)

// RecoverySuggestion proposes a way to fix the error
type RecoverySuggestion struct {
	// Approach: brief name for the suggestion
	Approach string `json:"approach"`
	
	// Description: human-readable explanation
	Description string `json:"description"`
	
	// Command: gorefactor command to try instead (optional)
	Command string `json:"command,omitempty"`
	
	// Likelihood: confidence that this will work (0.0-1.0)
	Likelihood float64 `json:"likelihood"`
	
	// Note: any caveats or additional info
	Note string `json:"note,omitempty"`
}

// DetailedError provides structured error information for LLM consumption
type DetailedError struct {
	// Code: machine-readable error classification
	Code ErrorCode `json:"code"`
	
	// Message: human-readable error message
	Message string `json:"message"`
	
	// Details: context-specific information
	Details map[string]interface{} `json:"details,omitempty"`
	
	// Context: the problematic code/location
	Context *ErrorContext `json:"context,omitempty"`
	
	// RootCauses: why this error occurred
	RootCauses []string `json:"rootCauses,omitempty"`
	
	// Suggestions: ways to fix the problem
	Suggestions []RecoverySuggestion `json:"suggestions,omitempty"`
	
	// RelatedCode: snippets of relevant code
	RelatedCode map[string]string `json:"relatedCode,omitempty"`
}

// ErrorContext provides location and code information
type ErrorContext struct {
	File        string `json:"file"`
	LineStart   int    `json:"lineStart"`
	LineEnd     int    `json:"lineEnd"`
	Description string `json:"description"`
}

// MarshalJSON implements json.Marshaler for compatibility
func (de *DetailedError) MarshalJSON() ([]byte, error) {
	type Alias DetailedError
	return json.Marshal(&struct {
		*Alias
		Code string `json:"code"`
	}{
		Alias: (*Alias)(de),
		Code:  string(de.Code),
	})
}

// Error implements the error interface
func (de *DetailedError) Error() string {
	return de.Message
}

// NewDetailedError creates a DetailedError with sensible defaults
func NewDetailedError(code ErrorCode, message string) *DetailedError {
	return &DetailedError{
		Code:       code,
		Message:    message,
		Details:    make(map[string]interface{}),
		RootCauses: []string{},
		Suggestions: []RecoverySuggestion{},
		RelatedCode: make(map[string]string),
	}
}

// WithContext adds location information
func (de *DetailedError) WithContext(file string, lineStart, lineEnd int, desc string) *DetailedError {
	de.Context = &ErrorContext{
		File:        file,
		LineStart:   lineStart,
		LineEnd:     lineEnd,
		Description: desc,
	}
	return de
}

// WithRootCause adds an explanation
func (de *DetailedError) WithRootCause(cause string) *DetailedError {
	de.RootCauses = append(de.RootCauses, cause)
	return de
}

// WithSuggestion adds a recovery option
func (de *DetailedError) WithSuggestion(approach, description string, likelihood float64) *DetailedError {
	de.Suggestions = append(de.Suggestions, RecoverySuggestion{
		Approach:   approach,
		Description: description,
		Likelihood: likelihood,
	})
	return de
}

// WithSuggestionCommand adds a recovery option with a command
func (de *DetailedError) WithSuggestionCommand(approach, description, command string, likelihood float64) *DetailedError {
	de.Suggestions = append(de.Suggestions, RecoverySuggestion{
		Approach:    approach,
		Description: description,
		Command:     command,
		Likelihood:  likelihood,
	})
	return de
}

// WithDetail adds context-specific information
func (de *DetailedError) WithDetail(key string, value interface{}) *DetailedError {
	de.Details[key] = value
	return de
}

// WithRelatedCode adds a code snippet
func (de *DetailedError) WithRelatedCode(label, code string) *DetailedError {
	de.RelatedCode[label] = code
	return de
}
```

### 2. Update `mutationResult` to Include Detailed Error

**File**: `cmd/gorefactor/mutation.go` (modify)

```go
// mutationResult is the shared --json result shape for mutation commands.
type mutationResult struct {
	Success       bool            `json:"success"`
	Operation     string          `json:"operation"`
	File          string          `json:"file,omitempty"`
	Detail        string          `json:"detail,omitempty"`
	FilesChanged  []string        `json:"filesChanged,omitempty"`
	LinesChanged  int             `json:"linesChanged"`
	UndoToken     string          `json:"undoToken,omitempty"`
	DryRun        bool            `json:"dryRun,omitempty"`
	Diff          string          `json:"diff,omitempty"`
	Error         string          `json:"error,omitempty"`
	ErrorDetails  *DetailedError  `json:"errorDetails,omitempty"`  // NEW
	Candidates    []string        `json:"candidates,omitempty"`
}

// Update fail() to extract DetailedError
func (m *mutation) fail(err error) error {
	result := mutationResult{
		Success:    false,
		Operation:  m.op,
		File:       m.file,
		Error:      err.Error(),
		Candidates: errCandidates(err),
	}
	
	// NEW: Extract DetailedError if present
	if de, ok := err.(*DetailedError); ok {
		result.ErrorDetails = de
	}
	
	if m.jsonOut {
		emitJSON(result)
	}
	return err
}
```

### 3. Example: Enhanced Extract Error Handling

**File**: `cmd/gorefactor/cmd_extract_extract.go` (modify)

Current:
```go
if containsReturn(blockStmts) {
    return m.fail(fmt.Errorf("block contains a return statement; v1 extract does not handle this"))
}
```

Enhanced:
```go
if containsReturn(blockStmts) {
    err := NewDetailedError(ErrReturnStatementInBlock, 
        "Cannot extract: block contains return statement")
    err.WithContext(file, startLine, endLine, 
        "Extraction range includes return").
       WithRootCause(
        "Return statements in extracted code are ambiguous - unclear whether to return from extracted function or caller").
       WithSuggestion("expand_to_statement", 
        "Extract the complete statement containing the return", 0.8).
       WithSuggestion("remove_return",
        "Refactor to use a value return instead of early return", 0.6).
       WithSuggestion("skip_extraction",
        "Extract a different block without return statements", 0.9).
       WithRelatedCode("problematic_block",
        fmt.Sprintf("Lines %d-%d contain return statement", startLine, endLine))
    return m.fail(err)
}
```

### 4. Scope Error Example (Most Common)

**File**: New function `analyzeExtractionErrors()` in `cmd_extract_extract.go`

```go
// analyzeExtractionErrors provides detailed feedback when extraction fails due to scope issues
func analyzeExtractionErrors(file string, enclosing *ast.FuncDecl, blockStmts []ast.Stmt, 
    params []*types.Var, returns *types.Tuple) *DetailedError {
    
    // Check for undefined variables
    undefinedVars := checkUndefinedVariables(enclosing, blockStmts, params)
    if len(undefinedVars) > 0 {
        err := NewDetailedError(ErrVariableOutOfScope,
            fmt.Sprintf("Cannot extract: %d variable(s) not in scope: %s",
                len(undefinedVars), strings.Join(undefinedVars, ", ")))
        
        for _, varName := range undefinedVars {
            def := findVariableDefinition(enclosing, varName)
            err.WithRootCause(
                fmt.Sprintf("%s is defined at line %d, outside extraction range %d-%d",
                    varName, def.Line, blockStmts[0].Pos(), blockStmts[len(blockStmts)-1].End()))
        }
        
        // Suggestion 1: Add as parameter
        cmd := fmt.Sprintf("gorefactor change-signature %s %s --add-param \"%s %s\"",
            file, enclosing.Name.Name, undefinedVars[0], 
            guessType(undefinedVars[0]))
        err.WithSuggestionCommand("add_parameter",
            "Add variable as parameter to extracted function",
            cmd, 0.95)
        
        // Suggestion 2: Expand extraction range
        newStart := findLineOfVariable(undefinedVars[0], enclosing)
        err.WithSuggestion("expand_range",
            fmt.Sprintf("Include variable definition in range (start at line %d)", newStart),
            0.8)
        
        // Suggestion 3: Make it package-level
        err.WithSuggestion("make_global",
            "Promote variable to package level if appropriate",
            0.3)
        
        return err.WithDetail("undefinedVariables", undefinedVars).
                   WithDetail("suggestedRange", 
                       map[string]int{"start": newStart, "end": endLine})
    }
    
    return nil
}
```

---

## Implementation Phases

### Phase 1: Core Infrastructure (1 day)

**Files to create**:
- `cmd/gorefactor/error_context.go` - `DetailedError` type and helpers

**Files to modify**:
- `cmd/gorefactor/mutation.go` - Update `mutationResult`, `fail()`
- `cmd/gorefactor/main.go` - Ensure JSON output is structured

**Testing**:
- Unit tests for `DetailedError` marshaling
- Verify JSON output is valid

### Phase 2: Extract Command (1 day)

**Files to modify**:
- `cmd/gorefactor/cmd_extract_extract.go` - Add error analysis
- `cmd/gorefactor/cmd_extract_analyze.go` - Enhance error handling

**Key improvements**:
1. Variable out of scope → suggestions for parameter or expansion
2. Return statement in block → explain and suggest alternatives
3. Invalid range → suggest valid ranges
4. Type mismatch → explain type incompatibility

**Testing**:
- Test each error condition
- Verify suggestions are actionable
- Test JSON output parsing

### Phase 3: Other Mutation Commands (1 day)

**Files to modify**:
- `cmd/gorefactor/cmd_direct.go` - Move/delete errors
- `cmd/gorefactor/cmd_insert.go` - Insert location errors
- `cmd/gorefactor/cmd_replace_body.go` - Replace errors

**Similar improvements** for each command

**Testing**:
- Test common error paths
- Verify pi can parse all error responses

### Phase 4: Pi Integration & Testing (Optional, 1 day)

**Deliverable**: Example pi session showing autonomous error recovery

---

## Key Error Patterns to Handle

### Extract Command
1. **Variable Out of Scope** → Suggest parameters or expand range
2. **Return Statement in Block** → Suggest alternatives
3. **Invalid Range** → Suggest corrected range
4. **Type Mismatch** → Explain expected types
5. **Undefined Function Call** → Suggest imports

### Move Command
1. **Circular Import** → Show import cycle
2. **Missing Type Definition** → Suggest moving type first
3. **Breaking Change** → Show affected callers
4. **Import Conflict** → Suggest renaming

### Insert Command
1. **Invalid Insertion Point** → Suggest alternatives
2. **Syntax Error** → Show what's wrong
3. **Scope Conflict** → Explain issue

### Delete Command
1. **Has Callers** → Show all callers
2. **Used in Tests** → Show test locations
3. **Part of Interface** → Explain implications

---

## Output Format Examples

### Example 1: Extraction Fails (Variable Out of Scope)

```json
{
  "success": false,
  "operation": "extract",
  "file": "order.go",
  "error": "Cannot extract: variable 'config' not in scope",
  "errorDetails": {
    "code": "VARIABLE_OUT_OF_SCOPE",
    "message": "Cannot extract: variable 'config' not in scope",
    "context": {
      "file": "order.go",
      "lineStart": 50,
      "lineEnd": 75,
      "description": "Extraction range lacks definition of 'config'"
    },
    "rootCauses": [
      "config is defined at line 40, outside extraction range 50-75",
      "Extracted function has no way to access config"
    ],
    "details": {
      "undefinedVariables": ["config"],
      "configDefinedAt": 40,
      "suggestedRange": {"start": 40, "end": 75}
    },
    "suggestions": [
      {
        "approach": "add_parameter",
        "description": "Add config as parameter to extracted function",
        "command": "gorefactor change-signature order.go validateOrder --add-param 'config OrderConfig' --position 0",
        "likelihood": 0.95
      },
      {
        "approach": "expand_range",
        "description": "Include config definition in extraction (lines 40-75)",
        "likelihood": 0.8,
        "note": "Only works if config assignment is simple"
      },
      {
        "approach": "make_global",
        "description": "Promote config to package-level variable",
        "likelihood": 0.4,
        "note": "Changes program structure; use only if config is truly global"
      }
    ],
    "relatedCode": {
      "extraction_range": "lines 50-75: validation logic",
      "config_definition": "line 40: config := loadConfig(...)"
    }
  }
}
```

### Example 2: Move Would Create Import Cycle

```json
{
  "success": false,
  "operation": "move",
  "file": "handlers.go",
  "error": "Cannot move: would create circular import",
  "errorDetails": {
    "code": "IMPORT_CYCLE",
    "message": "Cannot move ErrorHandler to errors.go: would create circular import",
    "rootCauses": [
      "handlers.go imports from types.go",
      "Moving ErrorHandler to errors.go would require errors.go to import from handlers.go",
      "This creates: handlers.go -> types.go and errors.go -> handlers.go"
    ],
    "suggestions": [
      {
        "approach": "move_dependencies_first",
        "description": "First move ErrorType to errors.go, then move ErrorHandler",
        "command": "gorefactor move handlers.go ErrorType errors.go",
        "likelihood": 0.9
      },
      {
        "approach": "consolidate_file",
        "description": "Merge handlers.go and errors.go into single file",
        "likelihood": 0.6
      },
      {
        "approach": "extract_shared",
        "description": "Move shared types to a new types.go, breaking the cycle",
        "likelihood": 0.7
      }
    ],
    "details": {
      "importGraph": {
        "handlers.go": ["types.go", "log"],
        "errors.go": ["fmt"],
        "would_create": "errors.go -> handlers.go"
      }
    }
  }
}
```

---

## Pi Integration Example

```go
// This is what pi would do with better errors

func refactorWithAutoRecovery(userRequest string) {
    // User: "Extract validateOrder from ProcessOrder into validators.go"
    
    // Step 1: Try extraction
    result := gorefactor.extract("orders.go", 50, 75, "validateOrder")
    
    if !result.Success && result.ErrorDetails != nil {
        err := result.ErrorDetails
        
        // Pi understands what went wrong
        if err.Code == "VARIABLE_OUT_OF_SCOPE" {
            undefined := err.Details["undefinedVariables"].([]string)
            
            // Pi picks the best suggestion
            best := err.Suggestions[0]  // Sorted by likelihood
            
            // Pi executes the recovery command
            log.Printf("Variable %s out of scope, adding as parameter...", undefined[0])
            output := exec.Command(best.Command).Output()
            
            // Pi tries again
            result2 := gorefactor.extract("orders.go", 50, 75, "validateOrder")
            if result2.Success {
                log.Printf("✓ Extraction succeeded after adding parameter")
            }
        }
    }
}
```

---

## Success Criteria

✅ All mutation commands return structured error responses  
✅ Each error has 2+ actionable suggestions  
✅ Suggestions are ordered by likelihood of success  
✅ JSON output is parseable by pi  
✅ Error messages are human-friendly  
✅ Code examples show the problem  
✅ Pi can execute suggested commands autonomously  

---

## Files to Create/Modify Summary

### Create (New)
- `cmd/gorefactor/error_context.go` - Error structures

### Modify
- `cmd/gorefactor/mutation.go` - Add DetailedError to result
- `cmd/gorefactor/cmd_extract_extract.go` - Enhanced extract errors
- `cmd/gorefactor/cmd_extract_analyze.go` - Analysis helpers
- `cmd/gorefactor/cmd_direct.go` - Move/delete errors
- `cmd/gorefactor/cmd_insert.go` - Insert errors
- `cmd/gorefactor/cmd_replace_body.go` - Replace errors
- `cmd/gorefactor/main.go` - Ensure JSON is formatted

### Add Tests
- `cmd/gorefactor/error_context_test.go` - Error marshaling
- `cmd/gorefactor/cmd_extract_extract_test.go` - Extract error cases
- Integration tests for error suggestions

---

**Next Step**: Start with Phase 1 implementation. Ready?
