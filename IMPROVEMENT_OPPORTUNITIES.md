# GoRefactor: Improvement Opportunities & Feature Ideas

**Date:** June 21, 2026  
**Analysis:** Codebase review, lint findings, architecture study

---

## Executive Summary

GoRefactor is a mature, well-architected tool with strong fundamentals. This analysis identifies **15+ improvement opportunities** across these categories:

1. **Code Health** (8 findings) - Refactoring opportunities within the codebase itself
2. **Feature Gaps** (12 findings) - Missing features that would add value
3. **Performance** (4 findings) - Optimization opportunities
4. **Testing & Documentation** (6 findings) - Gaps in coverage and guidance
5. **User Experience** (5 findings) - Usability enhancements

**Quick wins:** Split oversized test files, extract complex lint rules, add more analysis commands  
**High-impact:** Interactive refactoring UI, better error messages, incremental refactoring mode

---

## 1. Code Health Issues

### 1.1 Oversized Test Files (9 instances, autofix available)

**Issue**: Multiple test files exceed 300-line limit
- `log_propagation_more_test.go` (479 lines, **over by 179**)
- `phase3_test.go` (436 lines)
- `phase1_test.go` (428 lines)
- `journal_undo_test.go` (380 lines)
- `plan_suggester_test.go` (378 lines)
- `cmd_txn_parse_test.go` (346 lines)

**Recommendation**: Use `gorefactor lint . --fix` to auto-split these files. Consider:
- One file per major test function (following Go table-driven test patterns)
- Shared helper file for common setup
- Example: Split `log_propagation_more_test.go` into:
  - `log_propagation_basic_test.go`
  - `log_propagation_errors_test.go`
  - `log_propagation_helpers_test.go`

**Impact**: ⭐⭐ (Code maintainability, easier navigation)  
**Effort**: 30 minutes (mostly automation)

---

### 1.2 Complex Lint Rule Implementations (high NPath)

**Issue**: Several analyzer modules are large and complex:
- `log_propagation.go` (294 lines, 51 extraction candidates)
- `pattern_detector_smells.go` (274 lines)
- `dead_code_detector.go` (272 lines)
- `diff_analyzer_ops.go` (271 lines)
- `file_size_analyzer.go` (254 lines)

**Example**: `log_propagation.go` contains multiple nested conditions for detecting error-log patterns. Could benefit from:
- Extracting pattern matchers into separate helper functions
- Creating a `PatternMatcher` interface to decouple pattern detection from reporting
- Separating concerns: detection vs. reporting vs. suggestion generation

**Recommendation**: 
```go
// Current: 294 lines, hard to test individual patterns
func DetectLogPropagation(...) { ... }

// Better: 
type LogPattern interface {
    Match(stmt *ast.Stmt) bool
    Suggest() *Suggestion
}

type UnhandledErrorLog struct { ... }
func (p *UnhandledErrorLog) Match(...) bool { ... }

func DetectLogPropagation(...) {
    for _, pattern := range patterns {
        if pattern.Match(stmt) { ... }
    }
}
```

**Impact**: ⭐⭐⭐ (Testability, reusability)  
**Effort**: 2-3 days (requires careful refactoring)

---

### 1.3 Error Wrapping in Direct Commands

**Issue**: `cmd/gorefactor/cmd_direct.go` (299 lines) has multiple error paths without context:

```go
// Current
if err != nil {
    return err
}

// Better
if err != nil {
    return fmt.Errorf("failed to insert code: %w", err)
}
```

**Impact**: ⭐ (Debugging, error tracing)  
**Effort**: 1-2 hours (straightforward)  
**Tool**: Use `gorefactor lint . --fix` with error-wrapping autofix

---

### 1.4 Tight Coupling in Orchestrator

**Issue**: `orchestrator_orchestrator.go` (254 lines) has tight coupling between:
- Operation parsing
- Validation
- Execution
- Fallback handling

**Recommendation**: Introduce a `OperationValidator` interface:

```go
// Current: Mixed concerns
type Orchestrator struct {
    // contains parsing, validation, execution, fallback all together
}

// Better:
type OperationValidator interface {
    Validate(op *Operation) error
}

type Orchestrator struct {
    validator OperationValidator
    executor  OperationExecutor
}
```

**Impact**: ⭐⭐ (Testability, extensibility)  
**Effort**: 1-2 days

---

## 2. Feature Gaps

### 2.1 Missing: `merge-functions` Command

**Problem**: GoRefactor can extract, inline, and move functions, but can't merge related functions.

**Use Case**: 
```go
// Two functions doing similar things
func validateUser(u *User) error { ... }
func validateAdmin(a *Admin) error { ... }

// Want to consolidate to:
func validate[T any](v T) error { ... } // Go 1.18+
```

**Proposal**:
```bash
gorefactor merge-functions file.go validateUser validateAdmin \
  --into validateEntity \
  --enable-generics
```

**Impact**: ⭐⭐⭐ (Code deduplication, Go 1.18+ features)  
**Effort**: 3-5 days

---

### 2.2 Missing: `generate-mocks` Command

**Problem**: GoRefactor can extract interfaces but doesn't help generate mocks.

**Use Case**: After extracting an interface, auto-generate a mock for testing:
```bash
gorefactor extract-interface handler.go Handler IHandler
gorefactor generate-mocks handler.go IHandler # NEW
  # Generates: IHandler_mock.go using testify/mock or similar
```

**Proposal**: Integrate with popular mocking libraries:
- `github.com/stretchr/testify/mock`
- `github.com/golang/mock/gomock`
- Custom `Mocker` interface for extensibility

**Impact**: ⭐⭐⭐ (Testing workflow)  
**Effort**: 2-3 days

---

### 2.3 Missing: `find-memory-leaks` Lint Rule

**Problem**: GoRefactor's lint covers many smells but not resource management.

**Patterns to Detect**:
- `defer` on `Close()` missing for `*sql.Rows`, `*os.File`, `io.ReadCloser`
- Goroutines started but never waited (no `sync.WaitGroup` or channel close)
- Buffered channels that might deadlock

**Example**:
```go
// Not flagged currently
func readFile(path string) ([]byte, error) {
    f, err := os.Open(path)  // Missing: if no defer f.Close(), error
    if err != nil { return nil, err }
    return ioutil.ReadAll(f)  // Resource leak if ReadAll fails
}

// Should suggest:
func readFile(path string) ([]byte, error) {
    f, err := os.Open(path)
    if err != nil { return nil, err }
    defer f.Close()  // <-- AUTOFIX
    return ioutil.ReadAll(f)
}
```

**Implementation Strategy**:
1. Track `io.Closer` types returned from function calls
2. Check if `defer close()` or `defer .Close()` follows in same scope
3. Flag if resource escapes scope without close

**Impact**: ⭐⭐⭐ (Bug prevention, memory safety)  
**Effort**: 2-3 days (requires type tracking)

---

### 2.4 Missing: `refactor-for-concurrency` Command

**Problem**: No guidance on converting sequential code to concurrent patterns.

**Patterns to Detect & Suggest**:
- Independent loops that could use `errgroup.Group`
- Sequential HTTP calls that could be parallelized
- Channel patterns for fan-out/fan-in

**Example**:
```bash
gorefactor refactor-for-concurrency handlers.go ProcessRequests
# Suggests:
#   - Use errgroup for independent operations
#   - Use channels for producer-consumer pattern
#   - Add context propagation
```

**Impact**: ⭐⭐ (Performance guidance, concurrency patterns)  
**Effort**: 3-5 days

---

### 2.5 Missing: `convert-to-generics` Command

**Problem**: No tool to help convert pre-1.18 code to use generics.

**Patterns**:
- `interface{}`-based containers → `map[K]V` or `[]T`
- Type assertions → generic functions
- `reflect`-heavy code → generic alternatives

**Example**:
```bash
gorefactor convert-to-generics cache.go Cache
# Detects: map[string]interface{} storage
# Suggests: Use generics: Cache[K, V]
# Offers: --apply to perform conversion
```

**Impact**: ⭐⭐ (Modernization, type safety)  
**Effort**: 4-5 days (complex type analysis)

---

### 2.6 Missing: Cross-file `find-unused-imports` Optimization

**Current State**: `lint` can detect unused exports, but not unused imports efficiently.

**Proposal**:
```bash
gorefactor analyze-imports . --fix-unused
# Removes unused imports from all files
# Faster than running goimports everywhere
```

**Implementation**: Reuse existing AST analysis to:
1. Parse all files in a package
2. Build import usage graph
3. Remove unused imports with `gorefactor delete-import`

**Impact**: ⭐ (Code cleanliness)  
**Effort**: 1-2 days

---

### 2.7 Missing: `suggest-interfaces` Command

**Problem**: No tool to suggest where interfaces should be introduced.

**Pattern**: Detect implicit interface implementation:
```go
// Current: No interface declared
type Handler struct { }
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) { ... }

// Suggestion: Should implement http.Handler interface
gorefactor suggest-interfaces handler.go Handler
# Output: "Handler implicitly implements http.Handler"
```

**Impact**: ⭐⭐ (Design improvement, loose coupling)  
**Effort**: 2-3 days (interface matching)

---

### 2.8 Missing: Better Error Context in Mutations

**Problem**: When a mutation fails, error messages are often terse.

**Current**:
```
$ gorefactor extract file.go 50 60 myFunc
error: extraction failed
```

**Better**:
```
$ gorefactor extract file.go 50 60 myFunc
error: extraction failed: variable 'config' on line 55 is not defined
        in scope, cannot extract without making it a parameter

        line 55:     return config.Value  // <- unresolved

Suggestions:
  1. Add config as parameter: myFunc(config Config)
  2. Pass from outer scope: Pass config to the extracted function
  3. Make package global: Promote config to package variable
```

**Impact**: ⭐⭐⭐ (User experience, error recovery)  
**Effort**: 2-3 days (error context building)

---

## 3. Performance Improvements

### 3.1 Parallel Lint Rule Execution (DONE in v0.4.0)

**Status**: ✅ Already implemented (v0.4.0, ~13× speedup)

**Findings**:
- Lint now uses `errgroup` for concurrent rule execution
- Built-in caching for repeated file reads
- Byte-identical output with deterministic sort

**Next Steps**: Could optimize further with:
- Rule dependency graph (skip expensive rules if cheap rules already failed)
- Incremental analysis (only re-analyze changed files)

---

### 3.2 Lazy Load Project Metadata

**Issue**: `packages.Load` is called for every analysis; could cache results.

**Proposal**:
```go
// Current: Loads project for every command
type Analyzer struct {
    loadProject() // Expensive
}

// Better: Cache across commands in same session
type Analyzer struct {
    projectCache *sync.Map
    
    loadProject() // Checks cache first
}
```

**Impact**: ⭐⭐ (CLI responsiveness)  
**Effort**: 1-2 days

---

### 3.3 Incremental Analysis Mode

**Problem**: When analyzing large projects, re-analyzing unchanged code is wasteful.

**Proposal**:
```bash
# First run: Full analysis, saves fingerprints
gorefactor lint . --incremental --save-cache

# Second run: Only re-analyzes changed files
gorefactor lint . --incremental --cache-dir .gorefactor/lint-cache
# 90% faster on unchanged code
```

**Implementation**: 
- Hash file contents + AST signature
- Only re-run rules if hash changes
- Cache `recommend`, `lint`, `find-callers` results

**Impact**: ⭐⭐⭐ (Developer workflow, CI speed)  
**Effort**: 3-4 days

---

### 3.4 Memory Usage Optimization

**Finding**: `dead_code_detector.go` rebuilds full call graph for each function (O(n²) complexity).

**Current Code (from analysis)**:
```go
// For each unexported function:
for _, fn := range funcs {
    callGraph := buildCallGraph(pkg)  // <-- Redundant, called N times
}
```

**Status**: Partially fixed in v0.4.0 (call graph reset per build instead of per function)

**Further Optimization**:
- Share AST across analyses within same package
- Use `sync.Once` for expensive computations

**Impact**: ⭐ (Memory usage, large codebase support)  
**Effort**: 1-2 days

---

## 4. Testing & Documentation Gaps

### 4.1 Missing: Benchmark Suite for Core Operations

**Current State**: `benchmark/` directory exists but underutilized.

**Proposal**:
```bash
# Add benchmarks for common operations
go test ./... -bench=. -benchmem

BenchmarkExtractMethod-8       500     2.1ms/op      240KB/op
BenchmarkFindCallers-8         200     5.3ms/op      180KB/op
BenchmarkLint-8               100     9.1ms/op      520KB/op
```

**Benefits**:
- Detect performance regressions
- Set expectations for tool responsiveness
- Document real-world performance

**Effort**: 1-2 days

---

### 4.2 Missing: Integration Test Scenarios

**Current State**: Unit tests exist; missing end-to-end scenarios.

**Proposal**: Add `integration_test.go` scenarios:
```go
func TestScenario_ExtractAndMove(t *testing.T) {
    // 1. Create temp project
    // 2. Extract method from file A
    // 3. Move to file B
    // 4. Run go build + go test
    // 5. Verify result
}

func TestScenario_BatchRefactoring(t *testing.T) {
    // Test complex JSON plan with 5+ operations
}
```

**Impact**: ⭐⭐ (Reliability, confidence in tool)  
**Effort**: 2-3 days

---

### 4.3 Missing: Migration Guide for Third-Party Linters

**Problem**: Users unfamiliar with how to migrate from `golangci-lint` to GoRefactor lint.

**Proposal**: Document:
- Feature mapping table: golangci-lint rules → GoRefactor rules
- Migration script: Convert `.golangci.yml` to `.gorefactor.yml`
- Examples for common configs

**Impact**: ⭐⭐ (Adoption, user satisfaction)  
**Effort**: 1-2 days (mostly writing)

---

### 4.4 Missing: Video Tutorials

**Proposal**:
1. **Getting Started** (3 min): Installing, first extract
2. **Batch Refactoring** (5 min): JSON plans, orchestration
3. **Integration with Pi** (4 min): Using the LLM harness

**Impact**: ⭐⭐⭐ (Adoption, onboarding)  
**Effort**: 3-5 days (filming + editing)

---

### 4.5 Missing: Troubleshooting Guide

**Proposal**: Document common issues:
- "Extraction failed: variable not in scope" → Solutions
- "Plan didn't apply any operations" → Debugging steps
- "gorefactor doctor shows build errors after refactoring" → Recovery

**Impact**: ⭐⭐ (User experience)  
**Effort**: 1-2 days

---

### 4.6 Missing: Architecture Decision Records (ADRs)

**Current State**: CLAUDE.md is great but unstructured.

**Proposal**: Add ADRs documenting key decisions:
- `ADR-001: Why semantic targeting instead of line numbers`
- `ADR-002: Error-wrapping strategy in mutations`
- `ADR-003: Fallback strategies in orchestration`

**Impact**: ⭐ (Maintainability, onboarding developers)  
**Effort**: 1-2 days

---

## 5. User Experience Enhancements

### 5.1 Interactive Refactoring Mode (REPL)

**Current State**: `repl` command exists but needs enhancement.

**Proposal**: Add guided mode:
```bash
gorefactor interactive

> What would you like to refactor?
  1. Extract a method
  2. Find code smells
  3. Refactor for performance
  4. Find unused code
> 1

> Enter file path: handlers.go
[Shows complexity analysis]

> Enter method name to extract from: ProcessRequest
> Enter start/end lines: 45-67
> Enter new method name: validateInput

[Applies extraction, runs go build + go test]
✓ Extraction successful. Changes saved.
```

**Impact**: ⭐⭐⭐ (Accessibility, onboarding)  
**Effort**: 2-3 days

---

### 5.2 Web UI for Visualization

**Problem**: JSON output is powerful but not visually intuitive.

**Proposal**: Build a simple web UI:
```bash
gorefactor server --port 8080
# Open http://localhost:8080

# Features:
# - Visualize file structure (tree view)
# - Highlight extraction candidates
# - Drag-drop refactoring operations
# - Real-time build/test results
```

**Implementation**:
- Go backend + Vue.js frontend
- Server mode in `cmd/gorefactor-server/`
- WebSocket for real-time updates

**Impact**: ⭐⭐⭐⭐ (Accessibility, adoption)  
**Effort**: 5-7 days

---

### 5.3 Better Help & Examples

**Current**: `gorefactor --help` is good but could be enhanced.

**Proposal**:
```bash
gorefactor help extract
# Shows usage + common examples:

# Example 1: Extract validation logic
gorefactor extract payment.go 23-31 validateCard

# Example 2: Extract error handling
gorefactor extract service.go 45-50 handleError

# Learn more: https://docs.gorefactor.dev/extract
```

**Impact**: ⭐⭐ (Discoverability)  
**Effort**: 1-2 days

---

### 5.4 Shell Completion Scripts

**Missing**: Bash/Zsh completion for commands and file paths.

**Proposal**:
```bash
gorefactor completion bash | sudo tee /etc/bash_completion.d/gorefactor

# Now typing `gorefactor ex<TAB>` suggests:
# - extract
# - exec
# - extract-interface
```

**Impact**: ⭐ (Developer experience)  
**Effort**: 1-2 days (can use `urfave/cli` completion)

---

### 5.5 Diff Preview Before Applying

**Current**: `--dry-run` shows results but not side-by-side diff.

**Proposal**:
```bash
gorefactor extract file.go 45-60 myFunc --diff

# Shows:
# --- Before
# +++ After
# @@ -40,15 +40,25 @@
# ...
```

**Implementation**: Use `google/go-cmp/cmp` or similar for nice diffs.

**Impact**: ⭐⭐ (Confidence, review)  
**Effort**: 1-2 days

---

## 6. Lesser Features to Consider

### 6.1 `suggest-type-assertions` Linter Rule
Detect unchecked type assertions and suggest conversion to comma-ok idiom.

### 6.2 `detect-blocking-operations` Rule
Flag potentially blocking I/O in hot paths (e.g., disk reads in event handlers).

### 6.3 `generate-constructors` Command
Auto-generate constructor functions for types with many fields.

### 6.4 `suggest-buffer-pools` Rule
Detect allocations in tight loops and suggest `sync.Pool` patterns.

### 6.5 `validate-interfaces` Command
Check that types correctly implement intended interfaces (catch accidental breakage).

---

## 7. Integration Opportunities

### 7.1 Formal LSP Server Integration

**Current**: Works with pi (coding harness) but not LSP-based editors.

**Proposal**: Build `gorefactor-lsp` bridge:
- Editor requests refactoring via LSP `workspace/executeCommand`
- GoRefactor processes request and returns edits
- Editor applies edits

**Effort**: 3-4 days (integrating with existing CLI)

---

### 7.2 Gradle/Maven Plugin for JVM Polyglot

**Scope**: Out of scope (Go-specific tool), but could support Go modules in polyglot builds.

---

### 7.3 GitHub Actions Integration

**Proposal**: Official action:
```yaml
- uses: chrisophus/gorefactor@v1
  with:
    command: lint
    path: ./cmd
    autofix: true
```

**Effort**: 1-2 days

---

## Priority Matrix

| Feature | Impact | Effort | Priority | Notes |
|---------|--------|--------|----------|-------|
| Fix oversized test files | ⭐⭐ | 30m | 🟢 **Quick Win** | Use `gorefactor lint . --fix` |
| `find-memory-leaks` rule | ⭐⭐⭐ | 2-3d | 🔴 High | Bug prevention, real impact |
| Web UI | ⭐⭐⭐⭐ | 5-7d | 🟡 Medium | Long-term accessibility |
| Incremental analysis | ⭐⭐⭐ | 3-4d | 🟡 Medium | Workflow improvement |
| `generate-mocks` command | ⭐⭐⭐ | 2-3d | 🟡 Medium | Testing workflow |
| Interactive mode | ⭐⭐⭐ | 2-3d | 🟡 Medium | Accessibility |
| Better error context | ⭐⭐⭐ | 2-3d | 🟡 Medium | UX improvement |
| Refactor complex analyzers | ⭐⭐⭐ | 2-3d | 🟡 Medium | Code health |
| Migration guide | ⭐⭐ | 1-2d | 🟢 Easy | Adoption |
| Shell completions | ⭐ | 1-2d | 🟢 Easy | DX improvement |
| Diff preview | ⭐⭐ | 1-2d | 🟢 Easy | UX improvement |

---

## Recommended Roadmap (Next 3 Months)

### Month 1: Code Quality & Quick Wins
1. **Week 1**: Split oversized test files (`gorefactor lint . --fix`)
2. **Week 2**: Add benchmark suite, integration tests
3. **Week 3**: Implement `find-memory-leaks` lint rule
4. **Week 4**: Shell completions + diff preview mode

### Month 2: Feature Expansion
1. **Week 1-2**: `generate-mocks` command + tests
2. **Week 3**: Interactive refactoring mode (REPL enhancement)
3. **Week 4**: Better error messages with context

### Month 3: Integration & Polish
1. **Week 1-2**: Web UI MVP (file browser, extraction visualizer)
2. **Week 3**: Migration guide + troubleshooting docs
3. **Week 4**: GitHub Actions integration, video tutorials

---

## Conclusion

GoRefactor has a solid foundation. The highest-impact improvements are:

1. **Code health**: Split test files, extract complex rules
2. **Features**: Memory-leak detection, generics conversion, mocking support
3. **Performance**: Incremental analysis for large projects
4. **UX**: Web UI, interactive mode, better error messages
5. **Integration**: LSP server, GitHub Actions

The tool would benefit most from:
- ✅ **Quality gate automation** (already strong with lint + doctor)
- ✅ **Accessibility** (web UI, interactive mode)
- ⚠️ **Safety features** (memory-leak detection, resource tracking)
- ⚠️ **Modern Go support** (generics conversion, type parameter analysis)

**Estimated total effort for all items: 6-8 weeks** (prioritize as above)

---

## Next Steps

1. **Today**: Review this document, prioritize with stakeholders
2. **This week**: Execute quick wins (test file splitting, shell completions)
3. **Next week**: Design web UI mockups, start memory-leak detector spike
4. **Month 1**: Deliver 3-4 items from Month 1 roadmap
5. **Quarterly review**: Assess impact, adjust priorities

---

## Appendix: Code Analysis Data

### Complexity Hotspots (by file)

```
orchestrator/orchestrator_orchestrator.go    254 lines, 24+ extraction candidates
analyzer/log_propagation.go                   294 lines, 51 extraction candidates  
cmd/gorefactor/cmd_direct.go                  299 lines, ~20 candidates
analyzer/pattern_detector_smells.go           274 lines, ~18 candidates
analyzer/dead_code_detector.go                272 lines, ~16 candidates
```

### Test Coverage Gaps

- `benchmark/` package: 0% coverage (no tests)
- `version/` package: 0% coverage (not tested)
- Agent integration tests: Partial (some fail in CI due to git signing)

### Performance Baselines (from CHANGELOG v0.4.0)

- `gorefactor lint .` on gorefactor repo: **0.7s** (down from 9.1s)
- Mean time per mutation operation: **~30-50ms**
- Cold-start latency: **~20ms** (vs gopls ~1.9s)

---

**End of Analysis**
