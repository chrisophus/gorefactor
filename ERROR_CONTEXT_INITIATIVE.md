# Error Context Initiative: Full Implementation Plan

**Objective**: Enable autonomous LLM refactoring loops by providing structured error responses with actionable recovery suggestions.

**Status**: Phase 1 Complete ✅ | Phase 2-4 Ready for Implementation

**Total Duration**: 8-10 hours  
**Estimated Token Savings**: 10,000+ per complex refactoring

---

## What This Solves

### Before (Current State)
```
User: "Extract validation logic to validators.go"

Pi: Calls gorefactor extract
Response: error: Cannot extract: variable 'config' not in scope

Pi: "I don't know why or what to do"
User: Has to help manually
Pi: Tries random fixes
Result: 3-4 iterations, 100s of wasted tokens
```

### After (With This Initiative)
```
User: "Extract validation logic to validators.go"

Pi: Calls gorefactor extract --json
Response: {
  "errorDetails": {
    "code": "VARIABLE_OUT_OF_SCOPE",
    "suggestions": [{
      "approach": "add_parameter",
      "command": "gorefactor change-signature ...",
      "likelihood": 0.95
    }]
  }
}

Pi: "Variable 'config' is out of scope. I'll add it as a parameter."
Pi: Executes suggested command automatically
Pi: Retries extraction → Success! ✅
Result: 1 iteration, minimal token usage
```

---

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────┐
│                    gorefactor extract                        │
├─────────────────────────────────────────────────────────────┤
│                                                               │
│  ┌──────────────────┐                                        │
│  │ Try extraction   │                                        │
│  └────────┬─────────┘                                        │
│           │                                                  │
│      ┌────▼────┐                                             │
│      │ Success  │ ──→ Return mutationResult{success: true}   │
│      │ Failure  │ ──→ Continue below                         │
│      └────┬─────┘                                            │
│           │                                                  │
│  ┌────────▼────────────────────────────────────────┐        │
│  │ Analyze Error (NEW)                             │        │
│  │ • Identify error type (VARIABLE_OUT_OF_SCOPE)   │        │
│  │ • Collect context (file, lines, definitions)    │        │
│  │ • Generate suggestions (add param, expand, etc) │        │
│  │ • Sort by likelihood (0.95, 0.80, 0.30)        │        │
│  └────────┬───────────────────────────────────────┘        │
│           │                                                  │
│  ┌────────▼────────────────────────────────────────┐        │
│  │ Return DetailedError (NEW)                       │        │
│  │ • code: VARIABLE_OUT_OF_SCOPE                    │        │
│  │ • message: "Cannot extract: ..."                 │        │
│  │ • rootCauses: ["config defined outside range"]  │        │
│  │ • suggestions: [{approach, command, likelihood}]│        │
│  │ • details: {undefinedVariables, definitions}    │        │
│  └────────┬───────────────────────────────────────┘        │
│           │                                                  │
│  ┌────────▼────────────────────────────────────────┐        │
│  │ mutation.fail(err) (UPDATED)                    │        │
│  │ • Detect DetailedError                          │        │
│  │ • Include in mutationResult.errorDetails        │        │
│  │ • JSON output structure                          │        │
│  └────────┬───────────────────────────────────────┘        │
│           │                                                  │
│  ┌────────▼────────────────────────────────────────┐        │
│  │ JSON Response                                    │        │
│  │ { success: false, errorDetails: {...} }        │        │
│  └────────────────────────────────────────────────┘        │
│                                                               │
└─────────────────────────────────────────────────────────────┘

                         ↓

            ┌────────────────────────────────┐
            │  LLM/Pi Processes JSON          │
            ├────────────────────────────────┤
            │ 1. Parse errorDetails           │
            │ 2. Read root causes             │
            │ 3. Pick best suggestion (0.95)  │
            │ 4. Execute command              │
            │ 5. Retry original operation     │
            │ 6. Success or better error      │
            └────────────────────────────────┘
```

---

## Phase Breakdown

### Phase 1: Core Infrastructure ✅ COMPLETE

**What was done**:
- Created `DetailedError` type with JSON marshaling
- Defined semantic `ErrorCode` constants
- Created `RecoverySuggestion` structure
- Updated `mutation.go` to include `ErrorDetails`
- Written comprehensive tests (6 test cases, all passing)
- Helper functions for common errors

**Files**:
- ✅ `cmd/gorefactor/error_context.go` (380 lines)
- ✅ `cmd/gorefactor/error_context_test.go` (197 lines)
- ✅ `cmd/gorefactor/mutation.go` (updated)

**Status**: Production ready, tested, integrated

**Next**: Phase 2

---

### Phase 2: Extract Command (4-6 hours)

**What to do**:
- Implement variable scope analysis
- Add return statement detection
- Create detailed error messages
- Add 3+ recovery suggestions per error
- Comprehensive test coverage

**Key changes**:
- `cmd/gorefactor/cmd_extract_extract.go` → Add error analysis
- `cmd/gorefactor/cmd_extract_extract_test.go` → Add test cases

**Error types to handle**:
1. Undefined variables (VARIABLE_OUT_OF_SCOPE)
2. Return statements (RETURN_IN_BLOCK)
3. Invalid range (INVALID_RANGE)
4. Type conflicts (TYPE_CONFLICT)

**Example output when error occurs**:
```json
{
  "success": false,
  "errorDetails": {
    "code": "VARIABLE_OUT_OF_SCOPE",
    "message": "Cannot extract: variable 'config' not in scope",
    "suggestions": [
      {
        "approach": "add_parameter",
        "command": "gorefactor change-signature ... --add-param 'config Config'",
        "likelihood": 0.95
      },
      {
        "approach": "expand_range",
        "description": "Include config definition in extraction",
        "likelihood": 0.80
      }
    ]
  }
}
```

**Success criteria**:
- All extract errors use DetailedError
- 3+ suggestions for each error type
- Suggestions sorted by likelihood
- Test coverage > 80%
- Build succeeds, tests pass

---

### Phase 3: Other Mutation Commands (2-3 hours)

Apply same pattern to:
- **Move command** - Handle import cycles, type conflicts
- **Insert command** - Handle scope issues
- **Delete command** - Show callers, provide confirmation
- **Replace command** - Type checking

**Per command**: 30-45 minutes

---

### Phase 4: Pi Integration & Testing (2-3 hours)

- Test with actual pi sessions
- Verify LLM can parse and execute suggestions
- Create examples of autonomous recovery
- Document recovery patterns
- Optimize suggestion confidence scores

---

## Implementation Timeline

### Week 1
- **Mon**: Phase 1 infrastructure ✅ (DONE)
- **Tue**: Phase 2 extract command (4-6 hours)
- **Wed**: Phase 3 other commands (2-3 hours)
- **Thu**: Phase 4 integration & testing (2-3 hours)
- **Fri**: Documentation & optimization

**Total**: 8-10 hours of development

---

## Expected Impact

### Token Efficiency
| Scenario | Before | After | Savings |
|----------|--------|-------|---------|
| Extraction with undefined var | 500 tokens | 150 tokens | 70% |
| Complex refactor (5 steps) | 2000 tokens | 300 tokens | 85% |
| Batch refactoring (10 ops) | 5000 tokens | 600 tokens | 88% |

**Average savings: 75-80% tokens on failed operations**

### User Experience
- ✅ Autonomous error recovery (no human required)
- ✅ Faster iteration (3-5 seconds per fix vs minutes)
- ✅ Better error messages (LLM understands what went wrong)
- ✅ More reliable refactoring (LLM follows suggestions)

### Reliability
- ✅ Reduced retry attempts (1-2 vs 5-10)
- ✅ Lower failure rate (LLM chooses best suggestion)
- ✅ Graceful degradation (suggestions prioritized)

---

## How to Get Started

### Option 1: Continue Implementation Now
If you want to implement Phase 2-4 immediately:

```bash
cd /Users/ccason/sandbox/gorefactor

# Verify Phase 1
go test ./cmd/gorefactor/error_context_test.go -v

# Review Phase 2 plan
cat PHASE_2_IMPLEMENTATION_GUIDE.md

# Start Phase 2
# (Follow step-by-step guide in PHASE_2_IMPLEMENTATION_GUIDE.md)
```

### Option 2: Test Phase 1 First
If you want to verify Phase 1 works before continuing:

```bash
# Build with Phase 1 changes
go build -o gorefactor-test ./cmd/gorefactor

# Try a mutation command
./gorefactor-test extract test_file.go 10 20 testFunc --json

# Look for "errorDetails" field in JSON output
# (Currently empty for non-DetailedError cases)
```

### Option 3: Plan & Review
If you want to review before implementing:

1. Read `PHASE_1_COMPLETE.md` (current status)
2. Read `PHASE_2_IMPLEMENTATION_GUIDE.md` (next steps)
3. Review `error_context.go` (API design)
4. Review `error_context_test.go` (usage examples)

---

## Regression Testing

After each phase, verify:

```bash
# Build
go build -o gorefactor-new ./cmd/gorefactor

# Unit tests
go test ./cmd/gorefactor -v

# Integration tests
./gorefactor-new doctor

# Manual testing
./gorefactor-new lint .
./gorefactor-new recommend cmd/gorefactor/mutation.go --short
```

---

## Documentation Updates Needed

After Phase 1-4 complete, update:
- `README.md` - Mention error context feature
- `.pi/skills/gorefactor/SKILL.md` - How to use with pi
- `AGENTS.md` - Agent best practices
- API docs - JSON schema for errorDetails

---

## Success Metrics

### Technical
- ✅ All mutation commands return DetailedError on failure
- ✅ 2-3 suggestions per error (sorted by likelihood)
- ✅ JSON output valid and parseable
- ✅ 100+ test cases covering error paths

### User-Facing
- ✅ LLM can parse error responses
- ✅ LLM can execute suggested commands
- ✅ Error recovery is autonomous (no human help)
- ✅ ~75% token savings on failed operations

### Project Health
- ✅ Zero build warnings
- ✅ 90%+ test coverage on error paths
- ✅ Backward compatible (old clients still work)
- ✅ Well documented

---

## Files Modified/Created Summary

### Phase 1 ✅
- **Created**: `cmd/gorefactor/error_context.go` (380 LOC)
- **Created**: `cmd/gorefactor/error_context_test.go` (197 LOC)
- **Modified**: `cmd/gorefactor/mutation.go` (5 lines)
- **Status**: Complete, tested, integrated

### Phase 2-4 (Pending)
- **Modify**: `cmd/gorefactor/cmd_extract_extract.go`
- **Modify**: `cmd/gorefactor/cmd_direct.go`
- **Modify**: `cmd/gorefactor/cmd_insert.go`
- **Modify**: `cmd/gorefactor/cmd_replace_body.go`
- **Create**: Additional test files

---

## References

For detailed information, see:
- `IMPLEMENTATION_PLAN_ERROR_CONTEXT.md` - Complete specification
- `PHASE_1_COMPLETE.md` - What was accomplished
- `PHASE_2_IMPLEMENTATION_GUIDE.md` - How to continue
- `error_context.go` - API documentation via code comments
- `error_context_test.go` - Usage examples and test patterns

---

## Quick Links

**If you want to...**

📚 **Understand the design**
→ Read `IMPLEMENTATION_PLAN_ERROR_CONTEXT.md` (design section)

🏗️ **See what was built**
→ Read `PHASE_1_COMPLETE.md`

🔨 **Implement Phase 2**
→ Follow `PHASE_2_IMPLEMENTATION_GUIDE.md`

✅ **Verify Phase 1 works**
→ Run `go test ./cmd/gorefactor/error_context_test.go -v`

📖 **Review the code**
→ Read `cmd/gorefactor/error_context.go`

🧪 **See test examples**
→ Read `cmd/gorefactor/error_context_test.go`

---

## Questions?

Before asking, check:
1. Is it answered in `PHASE_2_IMPLEMENTATION_GUIDE.md`?
2. Is there an example in `error_context_test.go`?
3. Is it documented in `error_context.go` comments?

---

## Next Steps

### Immediately
✅ Phase 1 complete and ready to use

### This week
🔄 Phase 2: Implement extract command error handling (4-6 hours)

### Next week
🔄 Phase 3: Apply to move, insert, delete, replace (2-3 hours)

### Following week
🔄 Phase 4: Integration testing & optimization (2-3 hours)

---

## Summary

This initiative transforms error handling in gorefactor from:
- ❌ Cryptic error messages → ✅ Structured, actionable errors
- ❌ LLM gets stuck → ✅ LLM recovers autonomously
- ❌ 10+ retry attempts → ✅ 1-2 iterations
- ❌ 1000s of wasted tokens → ✅ 75-80% token savings

**Total effort**: 8-10 hours of implementation  
**Total value**: Hundreds of thousands of tokens saved across projects  
**Difficulty**: Low-medium (mostly following patterns)  

---

**Ready to proceed?** See `PHASE_2_IMPLEMENTATION_GUIDE.md` to start building Phase 2.

Or keep this as reference for the implementation journey.

---

**Document Status**: Final  
**Last Updated**: June 21, 2026  
**Phase 1 Completion**: 100% ✅
