# GoRefactor Code Review & Cleanup Plan

## Executive Summary

This document outlines a structured approach to reviewing and cleaning up the GoRefactor codebase. The plan identifies:
- **6 lint violations** (blockers for clean CI)
- **3 stub operations** (incomplete features)
- **49.6% orchestrator test coverage** (lowest critical package)
- **0% parser test coverage** (no tests at all)
- **Architectural opportunities** (semantic targeting extraction, operation handler refactoring)

**Recommended timeline:** 17-21 hours over 2-3 weeks (Phase 1 + 2), with optional architecture work in Phase 3.

---

## Current State Analysis

### Codebase Metrics
- **Total LOC:** ~10,341 (24 files)
- **Test LOC:** ~4,785 (80+ test functions)
- **Architecture:** 4 core packages + CLI + skill interface
- **Lint Issues:** 6 (3 errcheck, 3 staticcheck)

### Test Coverage by Package
| Package | Coverage | Status |
|---------|----------|--------|
| parser | 0% | ⚠️ No tests |
| analyzer | 85.7% | ✓ Good |
| extractor | 93.2% | ✓ Excellent |
| orchestrator | 49.6% | ⚠️ Low (critical package) |

### Architecture Assessment
| Package | Assessment | Size |
|---------|-----------|------|
| parser | ✓ Healthy - focused, single-purpose | 4.2K |
| analyzer | ✓ Healthy - well-designed, 5 specialized analyzers | 23K |
| extractor | ⚠️ Functional but incomplete (type inference TODO) | 4.9K |
| orchestrator | ⚠️ Large, mixed concerns, incomplete features, low coverage | 47K |

---

## Detailed Issue List

### Phase 1: Lint Violations (BLOCKING, ~30 minutes)

#### errcheck violations (code_inserter_test.go)
- **Lines 13, 67, 113:** `os.Remove()` error not checked in defer statements
- **Impact:** Could mask file cleanup failures in tests
- **Fix:** Wrap with error handling or log
- **Risk:** None (test code only)

#### staticcheck empty branches (orchestrator.go)
- **Lines 815, 860:** Empty error handling for `cmd.Run()` (goimports)
- **Current:** `if err := cmd.Run(); err != nil { }`
- **Fix:** Use `_ = cmd.Run()` or add explanatory comment
- **Impact:** Clearer intent that errors are intentionally ignored
- **Risk:** None (no logic change)

#### staticcheck type inference (orchestrator.go)
- **Line 696:** `var declIndex int = -1` should be `var declIndex = -1`
- **Impact:** Unnecessary explicit type declaration
- **Risk:** None (mechanical fix)

### Phase 2: Test Coverage Gaps (MEDIUM PRIORITY, 16-20 hours)

#### Parser Package Tests (4-6 hours)
- **Current:** 0% coverage, 0 tests
- **Why:** Parser is foundational; no tests is a risk
- **Scope:** 6 exported functions need test coverage
- **Target:** 90%+ coverage
- **Risk:** Low (parser is mature, stable code)

#### Orchestrator Test Coverage (6-8 hours)
- **Current:** 49.6% (lowest critical package)
- **Gap:** Semantic targeting logic, condition checking, operation dispatch
- **Target:** 80%+ coverage
- **Key Untested Paths:**
  - `findTargetBySemantics()` - complex pattern matching
  - `calculateSemanticScore()` - scoring algorithm
  - Condition evaluation & fallback strategies
- **Risk:** Medium (complex logic; may reveal edge cases)

#### Type Inference for Extractor (2-3 hours)
- **Current:** Defaults to `interface{}` when type uncertain (line 197 TODO)
- **Impact:** Reduces type safety for extracted methods
- **Improvement:** Infer actual types from variable usage patterns
- **Risk:** Medium (AST changes; must maintain syntax validity)

### Phase 3: Architectural Issues (DECISION REQUIRED, 12-16 hours)

#### Stub Operations (Incomplete Features)

**executeInlineMethod** (orchestrator.go:631-641)
- Status: Non-functional placeholder
- Current behavior: Returns success with no actual inlining
- Decision needed: Implement or remove?

**executeRenameVariable** (orchestrator.go:644-654)
- Status: Non-functional placeholder
- Current behavior: Returns success with no actual renaming
- Decision needed: Implement or remove?

**Recommended Decision: Remove (Option B)**
- **Rationale:** Token efficiency focus suggests keeping scope tight
- **Cost:** 1 hour (simple deletion)
- **Risk:** Very low
- **Trade-off:** Honest feature set; re-implement if users demand
- **Alternative:** Mark as `UnimplementedError` for explicit messaging

#### Orchestrator Package Size
- **Size:** 47K (orchestrator.go 33K + code_inserter.go 14K)
- **Concerns:**
  - Mixed concerns (execution, targeting, insertion, templating)
  - Low test coverage (49.6%)
  - 20+ methods in single struct
- **Opportunity:** Extract semantic targeting into separate package
  - ~500 LOC of pattern matching and scoring logic
  - Would improve testability and reusability
  - Minimal risk (refactoring, no logic change)

---

## Cleanup Tasks by Priority

### Immediate (Phase 1) - ~30 minutes
- [ ] Fix 3 errcheck violations in code_inserter_test.go
- [ ] Fix 2 empty branch warnings in orchestrator.go
- [ ] Fix type inference flag in orchestrator.go
- **Result:** Clean lint report, all CI checks pass

### Short-term (Phase 2) - 16-20 hours
- [ ] Add comprehensive tests for parser package (4-6h)
- [ ] Improve orchestrator test coverage to 80%+ (6-8h)
- [ ] Implement proper type inference in extractor (2-3h)
- **Result:** 90%+ parser coverage, 80%+ orchestrator coverage, fixed type safety

### Medium-term (Phase 3) - Variable (recommend 1h for Option B)
- [ ] Decide on stub methods (Recommend: Remove)
- [ ] Remove incomplete operations or mark as unimplemented
- [ ] Optional: Extract semantic targeting to separate package (4-5h)
- **Result:** Honest feature set, clearer architecture

### Optional (Phase 4) - 8-10 hours
- [ ] Document semantic targeting algorithm
- [ ] Add integration test suite
- [ ] Expand CLI documentation with examples

---

## Risk Assessment

### Low-Risk Tasks (Safe to Execute Immediately)
- Lint fixes (Phase 1)
- Parser tests
- Removing stub methods
- Code formatting

### Medium-Risk Tasks (Requires Testing)
- Orchestrator test coverage
- Type inference implementation
- Semantic targeting extraction

### Higher-Risk Tasks (Needs Careful Review)
- Implementing inline method
- Implementing rename variable

### Mitigation Strategies
1. Run full test suite after each phase
2. Run linter before committing
3. Create separate git branch per phase
4. Code review high-risk changes
5. Add integration tests before merging

---

## Success Criteria

### Phase 1 Completion
```bash
✓ golangci-lint run ./... → 0 issues
✓ make test passes
✓ make lint passes
```

### Phase 2 Completion
```bash
✓ Parser coverage ≥ 90%
✓ Orchestrator coverage ≥ 80%
✓ All tests pass
✓ Lint clean
```

### Phase 3 Completion (if Option B selected)
```bash
✓ No stub methods in codebase
✓ Clear operation registry
✓ Integration tests pass
```

---

## Critical Files for Implementation

1. **orchestrator/orchestrator.go** (33K)
   - Semantic targeting logic, operation dispatch, stub methods
   - Needs: Stub removal/documentation, test improvements

2. **orchestrator/code_inserter_test.go** (8.1K)
   - Lint violations (errcheck)
   - Needs: Error handling fixes

3. **parser/parser_test.go** (to create)
   - New test file for 0% coverage
   - Needs: ~15-20 comprehensive test cases

4. **extractor/extractor.go** (4.9K)
   - Type inference TODO (line 197)
   - Needs: Implementation of proper type detection

5. **analyzer/diff_analyzer.go** (23K)
   - Complex orchestration logic
   - Needs: Integration testing

---

## Timeline Recommendations

**Week 1: Phase 1** (30 min)
- Fix all lint issues
- Unblocks CI immediately

**Week 1-2: Phase 2** (16-20 hours)
- Parser tests (foundational)
- Orchestrator coverage
- Type inference

**Week 2-3: Phase 3** (~1 hour if Option B)
- Remove stub methods
- Document decision

**Week 3-4: Phase 4** (Optional)
- Documentation
- Integration tests
- Examples

**Total:** 17-21 hours over 2-3 weeks (recommended), up to 45 hours for complete cleanup

---

## Alignment with Token Efficiency Goals

This cleanup supports the token efficiency focus:

1. **Phase 1 (Lint)** - Ensures clean CI/CD pipeline
2. **Phase 2 (Coverage)** - Prevents regressions that require manual fixing
3. **Phase 3B (Remove Stubs)** - Honest feature set aligns with "minimal context" philosophy
   - No fake operations that pretend to work
   - Users know exactly what GoRefactor can do
4. **Phase 4 (Docs)** - Helps future Claude Code sessions understand semantic targeting

---

## Next Steps

1. **Review this plan** - Confirm phases and priorities
2. **Address Phase 1** immediately (lint fixes are blockers)
3. **Plan Phase 2** starting next week (test coverage improvements)
4. **Decide on Phase 3** approach (recommend Option B: remove stubs)
5. **Track completion** in git branches/PRs per phase
