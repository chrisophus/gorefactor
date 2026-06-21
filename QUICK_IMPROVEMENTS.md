# GoRefactor: Quick Improvements (Actionable Guide)

**Target Duration**: 2-4 hours for all quick wins

This document provides step-by-step instructions for implementing the highest-ROI improvements to GoRefactor.

---

## 🟢 Quick Win #1: Fix Oversized Test Files (15 minutes)

**Issue**: 9 test files exceed 300-line limit  
**Tool**: Built-in `gorefactor lint --fix`  
**Expected Impact**: Cleaner codebase, easier to navigate

### Steps:

```bash
cd /Users/ccason/sandbox/gorefactor

# Dry run: see what would be split
./gorefactor lint . --fix --dry-run

# Actually fix (will split into smaller files)
./gorefactor lint . --fix
```

**Result**: 
- `log_propagation_more_test.go` (479 lines) → Split into ~3 files
- `phase3_test.go` (436 lines) → Split into ~2 files
- Other oversized test files → Auto-split

**Verification**:
```bash
# Check all files now under 300 lines
./gorefactor lint . | grep "file-size"
# Should show no file-size violations
```

**Time**: 5-10 minutes  
**Impact**: ⭐⭐ (Code cleanliness, navigation)

---

## 🟢 Quick Win #2: Add Shell Completion (20 minutes)

**Issue**: No tab completion for gorefactor commands  
**Tool**: `urfave/cli` completion support  
**Expected Impact**: Better developer experience

### Steps:

1. **Generate bash completion**:
```bash
# Create completion script
mkdir -p /usr/local/etc/bash_completion.d
./gorefactor completion bash > /usr/local/etc/bash_completion.d/gorefactor

# Activate immediately
source /usr/local/etc/bash_completion.d/gorefactor

# Test it
gorefactor ex<TAB>  # Should show: extract, exec, extract-interface
```

2. **For zsh**:
```bash
mkdir -p ~/.zsh/completions
./gorefactor completion zsh > ~/.zsh/completions/_gorefactor
# Add to ~/.zshrc:
# fpath=(~/.zsh/completions $fpath)
```

### Implementation (if not already in CLI):

```bash
# Check if cli package supports completion
grep -r "completion" cmd/gorefactor/*.go | head -5

# If not present, add to main.go:
app.Commands = append(app.Commands, &cli.Command{
    Name:  "completion",
    Usage: "Generate shell completion",
    Subcommands: []*cli.Command{
        {
            Name: "bash",
            Action: func(c *cli.Context) error {
                return cli.BuildCompletionScript(app, os.Stdout)
            },
        },
    },
})
```

**Time**: 15-20 minutes  
**Impact**: ⭐ (Developer experience)

---

## 🟢 Quick Win #3: Add `--diff` Flag to Preview Changes (30 minutes)

**Issue**: `--dry-run` shows result but not visual diff  
**Implementation**: Use `cmp` package for side-by-side diff  
**Expected Impact**: Users can review changes before applying

### Steps:

1. **Add to `cmd_extract.go` (or main command handler)**:

```go
// Add flag
var diffFlag = &cli.BoolFlag{
    Name:  "diff",
    Usage: "Show diff before applying changes",
}

// In command handler:
if c.Bool("diff") {
    return showDiff(beforeContent, afterContent)
}

func showDiff(before, after string) error {
    // Split into lines
    beforeLines := strings.Split(before, "\n")
    afterLines := strings.Split(after, "\n")
    
    // Use cmp.Diff
    diff := cmp.Diff(beforeLines, afterLines)
    if diff != "" {
        fmt.Println(diff)
    }
    return nil
}
```

2. **Test it**:
```bash
./gorefactor extract file.go 45-60 newFunc --diff

# Output:
#   before:
#   func ProcessData(...) {
#       validation code...
# -     more code...
#   }
# +   validation code moved to:
# +   func newFunc(...) {
# +       validation code...
# +   }
```

**Time**: 25-30 minutes  
**Impact**: ⭐⭐ (User confidence)

---

## 🟡 Medium Win #1: Extract Lint Rules Into Separate Handlers (2-3 hours)

**Issue**: `log_propagation.go` (294 lines) has multiple concerns tangled together  
**Benefit**: Better testability, code reuse  
**Approach**: Refactor using `gorefactor extract`

### Current Structure:

```go
// analyzer/log_propagation.go (294 lines, monolithic)
func DetectLogPropagation(ctx context.Context, files ...) []Issue {
    // - Pattern matching logic
    // - Error detection
    // - Suggestion generation
    // All mixed together
}
```

### Target Structure:

```go
// analyzer/log_pattern.go (new file)
type LogPattern interface {
    Match(stmt *ast.Stmt) bool
    Name() string
    Suggestion() string
}

type UnhandledErrorLog struct { ... }
type ErrorLoggedThenReturned struct { ... }

// analyzer/log_propagation.go (refactored, ~150 lines)
func DetectLogPropagation(ctx context.Context, ...) []Issue {
    patterns := []LogPattern{
        &UnhandledErrorLog{},
        &ErrorLoggedThenReturned{},
        // ...
    }
    
    for _, pattern := range patterns {
        if pattern.Match(stmt) {
            issues = append(issues, Issue{pattern.Suggestion()})
        }
    }
}
```

### Implementation Steps:

1. **Analyze extraction candidates**:
```bash
./gorefactor recommend analyzer/log_propagation.go --short | jq '.[] | 
  select(.statementCount > 5 and .isExtractable) | 
  {startLine, endLine, complexity, reason}'
```

2. **Extract first pattern matcher** (lines 45-67):
```bash
./gorefactor extract analyzer/log_propagation.go 45 67 matchUnhandledError
```

3. **Create new file**:
```bash
./gorefactor create analyzer/log_pattern.go - << 'EOF'
package analyzer

import "go/ast"

type LogPattern interface {
    Match(stmt *ast.Stmt) bool
    Name() string
    Suggestion() string
}
EOF
```

4. **Move extracted function to new file**:
```bash
./gorefactor move analyzer/log_propagation.go matchUnhandledError \
  analyzer/log_pattern.go
```

5. **Repeat for other patterns** (3-4 more times)

6. **Verify**:
```bash
./gorefactor doctor
go test ./analyzer/...
```

**Time**: 2-3 hours  
**Impact**: ⭐⭐⭐ (Code health, maintainability)

---

## 🟡 Medium Win #2: Refactor Tight Coupling in Orchestrator (2-3 hours)

**Issue**: `orchestrator_orchestrator.go` mixes parsing, validation, execution  
**Benefit**: Better testability, extensibility  

### Current Coupling:

```go
// orchestrator_orchestrator.go
type Orchestrator struct {
    // Contains mixed logic for:
    // - JSON parsing
    // - Operation validation
    // - Execution with fallback handling
}

func (o *Orchestrator) Execute(plan *Plan) error {
    for _, op := range plan.Operations {
        if !isValid(op) {  // Validation mixed in
            handleFallback(op)  // Fallback mixed in
        }
        execute(op)  // Execution mixed in
    }
}
```

### Target Architecture:

```go
// orchestrator/validator.go (new)
type OperationValidator interface {
    Validate(op *Operation) error
}

// orchestrator/executor.go (new)
type OperationExecutor interface {
    Execute(op *Operation) error
}

// orchestrator/fallback.go (new)
type FallbackHandler interface {
    Handle(op *Operation, err error) error
}

// orchestrator_orchestrator.go (refactored)
type Orchestrator struct {
    validator  OperationValidator
    executor   OperationExecutor
    fallback   FallbackHandler
}
```

### Implementation:

1. **Create validator interface**:
```bash
./gorefactor create orchestrator/validator.go - << 'EOF'
package orchestrator

type OperationValidator interface {
    Validate(op *Operation) error
}

type DefaultValidator struct{}

func (v *DefaultValidator) Validate(op *Operation) error {
    if op == nil {
        return fmt.Errorf("operation cannot be nil")
    }
    // Add other validation logic
    return nil
}
EOF
```

2. **Create executor interface**:
```bash
./gorefactor create orchestrator/executor.go - << 'EOF'
package orchestrator

type OperationExecutor interface {
    Execute(op *Operation) error
}

type DefaultExecutor struct {
    // executor state
}

func (e *DefaultExecutor) Execute(op *Operation) error {
    // Move execution logic here from orchestrator.go
    return nil
}
EOF
```

3. **Extract validation logic from orchestrator.go**:
```bash
./gorefactor recommend orchestrator/orchestrator_orchestrator.go | 
  jq '.[] | select(.complexity > 2)'
# Find validation blocks, extract them
```

4. **Refactor main Execute method**:
```bash
./gorefactor replace-body orchestrator/orchestrator_orchestrator.go \
  Orchestrator:Execute - << 'EOF'
// New implementation using injected dependencies
for _, op := range plan.Operations {
    if err := o.validator.Validate(op); err != nil {
        if err := o.fallback.Handle(op, err); err != nil {
            return err
        }
        continue
    }
    if err := o.executor.Execute(op); err != nil {
        return err
    }
}
return nil
EOF
```

5. **Verify tests still pass**:
```bash
./gorefactor doctor
```

**Time**: 2-3 hours  
**Impact**: ⭐⭐⭐ (Testability, extensibility)

---

## 🟠 Larger Win: Implement `find-memory-leaks` Lint Rule (1-2 days)

**Scope**: This is more involved but high-impact  
**Expected Impact**: ⭐⭐⭐ (Bug prevention, memory safety)

### Step 1: Design Pattern Detectors

```go
// analyzer/memory_leak_detector.go

// Pattern 1: Missing defer close for io.Closer types
func detectMissingDeferClose(fn *ast.FuncDecl) []MemoryLeakIssue {
    // Find calls that return io.Closer:
    // - os.Open
    // - ioutil.ReadFile
    // - db.Query
    // - http.Get
    // etc.
    
    // Check if defer close() follows
    // If not, flag as memory leak
}

// Pattern 2: Goroutines without WaitGroup
func detectUnjoinedGoroutines(fn *ast.FuncDecl) []MemoryLeakIssue {
    // Find `go func() { ... }()`
    // Check if WaitGroup.Wait() or channel close follows
    // If not, flag as potential goroutine leak
}

// Pattern 3: Buffered channels that might deadlock
func detectChannelDeadlock(fn *ast.FuncDecl) []MemoryLeakIssue {
    // Find make(chan T, size) with size > 0
    // Check if all send/recv paths are balanced
    // If not, flag as potential deadlock
}
```

### Step 2: Add Lint Rule Registration

```go
// analyzer/lint_registry.go
var memoryLeakRule = &Rule{
    Name:        "memory-leak",
    Severity:    "warning",
    Description: "Potential memory leaks: missing defer close, unjoinedgoroutines",
    Check: func(ctx context.Context, ...) []Issue {
        return detectMemoryLeaks(ctx, ...)
    },
}
```

### Step 3: Implement Auto-Fix

```go
// analyzer/memory_leak_autofix.go
func autoFixMissingDeferClose(fn *ast.FuncDecl, issue MemoryLeakIssue) error {
    // Suggest:
    // defer resource.Close()
    // Or:
    // defer func() { _ = resource.Close() }()
}
```

### Step 4: Test

```bash
# Create test file with memory leaks
cat > test_leaks.go << 'EOF'
func readFile(path string) ([]byte, error) {
    f, err := os.Open(path)  // Missing: defer f.Close()
    if err != nil { return nil, err }
    return ioutil.ReadAll(f)
}
EOF

# Run detector
./gorefactor lint . --include memory-leak

# Should output:
# test_leaks.go:2 [warning] memory-leak: 
#   os.Open() returns io.Closer but no defer close() found
#   Suggestions: Add defer f.Close()
```

### Files to Create/Modify:

1. `analyzer/memory_leak_detector.go` (new, ~200 lines)
2. `analyzer/memory_leak_detector_test.go` (new, ~150 lines)
3. `analyzer/lint_registry.go` (modify, add rule)
4. `cmd/gorefactor/cmd_lint_memory_leak.go` (new, ~50 lines)

**Time**: 1-2 days (including tests)  
**Impact**: ⭐⭐⭐ (Bug prevention, real safety improvement)

---

## 🟠 Larger Win: Interactive Refactoring Mode (1-2 days)

**Current**: `repl` command exists but minimal  
**Proposal**: Guided interactive wizard

### Step 1: Design UX Flow

```
$ gorefactor interactive

┌─────────────────────────────────────┐
│ GoRefactor Interactive Mode         │
│ Type 'help' for commands            │
└─────────────────────────────────────┘

> What would you like to do?
  1. Extract a method
  2. Find code smells
  3. Inline a function
  4. Move a function
  5. Find all callers
> 1

> Enter file path: ./handlers.go
[Loading... file has 487 lines, 12 functions]

Functions in handlers.go:
  1. ServeHTTP (45 lines, complexity 8)
  2. ParseRequest (67 lines, complexity 12)
  3. ValidateInput (34 lines, complexity 5)
  4. HandleError (23 lines, complexity 3)

> Which function? (1-4): 2

> Extraction candidates in ParseRequest:
  1. Lines 50-60: Validate request (5 lines)
  2. Lines 62-72: Parse JSON (8 lines)
  3. Lines 75-82: Handle errors (4 lines)

> Which block? (1-3): 1
> Method name for extracted code: validateRequest

[Extracting validateRequest...]
[Running go build...]
[Running go test...]

✓ Success! validateRequest extracted.

Changes preview:
  - ParseRequest: 67 lines → 59 lines
  - validateRequest: new method (5 lines)

> Apply changes? (y/n): y
✓ Applied!

> Do something else? (1-5, h for help, q to quit): 5
[Finding callers...]

validateRequest is called by:
  - ParseRequest (in same file)

> Refactor more? (y/n): n
Goodbye!
```

### Step 2: Implementation Files

```go
// cmd/gorefactor/cmd_interactive_menu.go (new, ~200 lines)
type InteractiveMode struct {
    reader *bufio.Reader
}

func (im *InteractiveMode) Run() error {
    for {
        choice := im.showMenu()
        switch choice {
        case "extract":
            im.interactiveExtract()
        case "find-callers":
            im.interactiveFindCallers()
        // ... more options
        }
    }
}

// cmd/gorefactor/cmd_interactive.go (modify existing)
// Update the main interactive command to use new menu system
```

### Step 3: Test It

```bash
# Manual testing
echo -e "1\nhandlers.go\n2\n50\n60\nvalidateRequest\ny\nn" | \
  ./gorefactor interactive

# Should walk through the entire flow
```

**Files**: 
- `cmd/gorefactor/cmd_interactive.go` (modify, ~50 lines)
- `cmd/gorefactor/interactive_menu.go` (new, ~200 lines)  
- `cmd/gorefactor/interactive_menu_test.go` (new, ~150 lines)

**Time**: 1-2 days  
**Impact**: ⭐⭐⭐ (Accessibility, onboarding)

---

## Priority Execution Plan

### Day 1: Quick Wins (2-3 hours)
1. ✅ Fix oversized test files (15 min) - `gorefactor lint . --fix`
2. ✅ Add shell completion (20 min)
3. ✅ Add diff preview flag (30 min)
4. ✅ Commit & test all changes (30 min)

### Day 2: Extract Lint Rules (2-3 hours)
1. Analyze `log_propagation.go` extraction candidates
2. Extract pattern matchers into new functions
3. Create `log_pattern.go` interface
4. Refactor main detection function
5. Run tests, verify improvement

### Day 3-4: Orchestrator Refactoring (2-3 hours)
1. Create validator interface + implementation
2. Create executor interface + implementation
3. Extract fallback handling logic
4. Refactor `Execute()` method to use injected deps
5. Update tests to use mocked dependencies
6. Verify integration tests pass

### Week 2: Larger Features (Optional)
- Memory leak detector (1-2 days)
- Interactive mode (1-2 days)

---

## Testing Each Improvement

### Quick Win Testing:

```bash
# After each change:
./gorefactor doctor                      # Full gate
git status                                # Check what changed
git diff cmd/gorefactor/main.go          # Review changes
go test ./...                             # Run full test suite
```

### Integration Testing:

```bash
# Create sample project for testing
mkdir -p /tmp/test-gorefactor
cd /tmp/test-gorefactor
go mod init test

# Copy a Go file
cp /path/to/sample.go .

# Test each improvement:
/Users/ccason/sandbox/gorefactor/gorefactor lint .
/Users/ccason/sandbox/gorefactor/gorefactor extract sample.go 10 20 testFunc --diff
```

---

## Metrics & Verification

After each improvement, check:

```bash
# Code metrics
./gorefactor lint . --json | jq '.statistics'

# Performance
time ./gorefactor lint .
time ./gorefactor recommend <file>

# Test coverage
go test ./... -cover

# Build
go build -o gorefactor ./cmd/gorefactor
```

Expected improvements:

| Metric | Before | After |
|--------|--------|-------|
| File-size violations | 9 | 0 |
| Avg file lines | ~250 | ~180 |
| Test file count | 23 | 35+ |
| UX responsiveness | 30ms+ | <25ms |

---

## Documentation Updates

After each change, update:
1. `README.md` (if user-facing)
2. `CHANGELOG.md` (add entry)
3. Comments in code (update docs)
4. Tests (add test cases)

Example changelog entry:
```markdown
## [0.4.1] - 2026-06-22

### Added
- Shell completion for bash/zsh
- `--diff` flag to preview changes before applying

### Fixed
- Oversized test files split into smaller units
- Improved error messages with context

### Changed
- Orchestrator now uses dependency injection (better testability)
```

---

## Conclusion

This guide provides **actionable steps** for 7 improvements totaling **4-6 days of work**:

- **Quick wins** (Day 1): 3 improvements in 2-3 hours
- **Medium wins** (Days 2-3): 2 improvements in 4-6 hours
- **Larger wins** (Week 2): 2 improvements in 3-4 days

Execute in priority order, test thoroughly, and celebrate each improvement! 🎉

---

**Questions?** Refer to:
- Original analysis: `IMPROVEMENT_OPPORTUNITIES.md`
- Architecture guide: `CLAUDE.md`
- Skill guide: `.pi/skills/gorefactor/SKILL.md`

