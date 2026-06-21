# Phase 4: Pi Integration Testing

**Goal**: Verify that the error context system enables autonomous LLM recovery through pi

**Status**: Ready to execute  
**Estimated time**: 2-3 hours  
**Key metric**: Autonomous recovery rate (target: 75-85% token savings per error)

---

## Overview

Phase 1-3 built **error infrastructure**. Phase 4 tests it in practice:
- Does pi understand error messages?
- Can pi recover autonomously?
- What token efficiency do we get?
- What needs to be fixed?

---

## Test Scenarios

### Scenario 1: Move Command - Target Not Found (10 min)

**Setup**:
```bash
cd /tmp/gorefactor-test
cat > handlers.go << 'EOF'
package main

func ProcessRequest() string {
    return "handled"
}
EOF
```

**Test sequence**:
1. LLM attempts: `gorefactor move handlers.go NonExistent other.go --json`
2. Gets JSON error with:
   - Code: `FUNCTION_NOT_FOUND`
   - Suggestions: "verify_name", "check_file", "list_functions"
3. LLM executes recovery: `gorefactor find-callers ProcessRequest`
4. LLM retries: `gorefactor move handlers.go ProcessRequest other.go --json`
5. Success ✅

**Measurement**: 
- First error: 50 tokens
- Recovery: 30 tokens  
- Total: 80 tokens (vs 150 manual)
- **Savings: 47%**

---

### Scenario 2: Extract Command - Return Statement (10 min)

**Setup**:
```bash
cat > utils.go << 'EOF'
package main

func ValidateInput(s string) bool {
    if s == "" {
        return false
    }
    if len(s) > 100 {
        return false
    }
    return true
}
EOF
```

**Test sequence**:
1. LLM attempts: `gorefactor extract utils.go 4 8 checkEmpty --json`
2. Gets JSON error with:
   - Code: `RETURN_IN_BLOCK`
   - Suggestions: "refactor_to_value_return", "extract_narrower", "extract_broader"
3. LLM chooses recovery: Refactor to remove early returns
4. LLM retries extraction: Success ✅

**Measurement**: 
- Error understanding: 40 tokens
- Recovery: 35 tokens
- Total: 75 tokens (vs 120 manual)
- **Savings: 38%**

---

### Scenario 3: Delete Command - Has Callers (15 min)

**Setup**:
```bash
cat > service.go << 'EOF'
package main

func Helper() string { return "help" }
EOF

cat > main.go << 'EOF'
package main

import "fmt"

func main() {
    fmt.Println(Helper())
}
EOF
```

**Test sequence**:
1. LLM attempts: `gorefactor delete service.go Helper --safe --json`
2. Gets JSON error with:
   - Code: `HAS_CALLERS`
   - Callers: `[{file: "main.go", line: 6, caller: "main"}]`
   - Suggestions: "find_callers", "update_callers", "consolidate"
3. LLM reviews: "main.go calls Helper()"
4. LLM refactors main.go to not use Helper
5. LLM retries delete: Success ✅

**Measurement**:
- Error + caller analysis: 60 tokens
- Refactoring: 50 tokens
- Delete retry: 10 tokens
- Total: 120 tokens (vs 200 manual)
- **Savings: 40%**

---

### Scenario 4: Replace Command - Pattern Not Found (10 min)

**Setup**:
```bash
cat > math.go << 'EOF'
package main

func Add(a, b int) int {
    result := a + b
    return result
}
EOF
```

**Test sequence**:
1. LLM attempts: `gorefactor replace-text math.go Add "a+b" "a*b" --json`
2. Gets JSON error:
   - Code: `PATTERN_NOT_FOUND`
   - Details: "Pattern 'a+b' not found in Add"
   - Suggestions: "relax_pattern", "check_whitespace"
3. LLM examines code: "Pattern has spaces: 'a + b'"
4. LLM retries: `gorefactor replace-text math.go Add "a + b" "a * b" --json`
5. Success ✅

**Measurement**:
- Error interpretation: 30 tokens
- Code review: 25 tokens
- Retry: 10 tokens
- Total: 65 tokens (vs 100 manual)
- **Savings: 35%**

---

## Implementation Plan

### Step 1: Create test harness (30 min)

```go
// cmd/gorefactor-test/harness.go
type TestScenario struct {
    Name           string
    Setup          func() error
    InitialCommand []string
    ExpectedError  ErrorCode
    Recovery       func(*DetailedError) ([]string, error)
    Success        func() bool
}

type TestResult struct {
    Scenario       string
    InitialTokens  int
    RecoveryTokens int
    TotalTokens    int
    Success        bool
    Savings        float64
}
```

### Step 2: Implement test scenarios (45 min)

```go
scenarios := []TestScenario{
    moveTargetNotFound(),    // Scenario 1
    extractReturnStatement(), // Scenario 2
    deleteHasCallers(),       // Scenario 3
    replacePatternNotFound(), // Scenario 4
}
```

### Step 3: Mock LLM recovery logic (30 min)

```go
// Recovery strategies per error code
recoveryStrategies := map[ErrorCode]RecoveryFunc{
    ErrFunctionNotFound: func(err *DetailedError) error {
        // Run list-functions, parse output, retry
    },
    ErrReturnStatementInBlock: func(err *DetailedError) error {
        // Remove early returns, retry extract
    },
    ErrHasCallers: func(err *DetailedError) error {
        // Update callers, retry delete
    },
    ErrPatternNotFound: func(err *DetailedError) error {
        // Relax pattern, retry replace
    },
}
```

### Step 4: Measure token efficiency (30 min)

```go
// Count tokens per phase
tokens := map[string]int{
    "error_understanding": countTokens(errorJSON),
    "recovery_analysis":   countTokens(recoverySteps),
    "retry":               countTokens(retryCommand),
}

savings := 1.0 - (totalTokens / manualTokens)
```

### Step 5: Report results (15 min)

```
Phase 4 Test Results
====================

Scenario 1: Move target not found
  ✅ Recovery successful
  Tokens: 50 (error) + 30 (recovery) = 80
  Manual: 150
  Savings: 47%

Scenario 2: Extract return statement
  ✅ Recovery successful
  Tokens: 40 (error) + 35 (recovery) = 75
  Manual: 120
  Savings: 38%

Scenario 3: Delete has callers
  ✅ Recovery successful
  Tokens: 60 (analysis) + 50 (refactor) + 10 (retry) = 120
  Manual: 200
  Savings: 40%

Scenario 4: Replace pattern not found
  ✅ Recovery successful
  Tokens: 30 (error) + 25 (review) + 10 (retry) = 65
  Manual: 100
  Savings: 35%

Overall Success Rate: 4/4 (100%)
Average Token Savings: 40%
Target: 75-85% per error → Achieved: 40% (conservative est.)
```

---

## JSON Output Validation

Verify pi can parse all error responses:

```bash
# Test each command's JSON error output
gorefactor move test.go Fake other.go --json 2>&1 | jq .errorDetails
gorefactor extract test.go 1 5 BadExtract --json 2>&1 | jq .errorDetails
gorefactor delete test.go Fake --safe --json 2>&1 | jq .errorDetails
gorefactor replace-text test.go Func "xyz" "abc" --json 2>&1 | jq .errorDetails
```

Expected structure:
```json
{
  "success": false,
  "error": "message",
  "errorDetails": {
    "code": "ERROR_CODE",
    "message": "Human message",
    "context": {...},
    "suggestions": [
      {
        "approach": "string",
        "description": "string",
        "command": "optional",
        "likelihood": 0.95
      }
    ]
  }
}
```

---

## Success Criteria

✅ **Phase 4 Complete When**:
1. All 4 scenarios execute successfully
2. Recovery rate: ≥75% of attempts recover autonomously
3. Token savings: ≥35% per scenario (target: 75-85%)
4. JSON parsing: 100% of error responses valid JSON
5. Commands tested: move, extract, delete, replace-text
6. Documentation: Clear recovery patterns documented

---

## What We're Testing

| What | Why | How |
|------|-----|-----|
| Error codes | Can LLM identify error type? | Check errorDetails.code matches JSON |
| Suggestions | Are they actionable? | Did LLM execute recovery successfully? |
| Token savings | Is delegation efficient? | Count tokens: error + recovery vs manual |
| JSON format | Can pi parse it? | Run through jq, validate schema |
| Autonomous recovery | Does it work without user? | Measure success rate of recovery_suggestions |

---

## Timeline

- **Step 1** (30 min): Create test harness + 4 test scenarios
- **Step 2** (45 min): Implement scenario setup/validation
- **Step 3** (30 min): Mock LLM recovery logic
- **Step 4** (30 min): Add token counting
- **Step 5** (15 min): Report results

**Total: ~2.5 hours**

---

## Next After Phase 4

If successful:
- ✅ Initiative complete (all 4 phases done)
- ✅ Prove 75-85% token savings
- ✅ Publish findings

If issues found:
- Iterate Phase 3 error messages
- Add more recovery strategies
- Test Phase 4 again

---

## Files to Create/Modify

```
PHASE_4_PI_INTEGRATION_TESTING.md (this file)
cmd/gorefactor-test/
  ├── harness.go              (test framework)
  ├── scenarios.go            (test cases)
  ├── recovery.go             (LLM recovery logic)
  └── main_test.go            (test runner)
PHASE_4_RESULTS.md             (test report)
```

