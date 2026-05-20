# GoRefactor Repository Code Quality Analysis

## Overview

A new **code quality analysis tool** (`gorefactor-repo-check`) has been created to detect code smells and issues that go beyond what regular linters can catch. This tool leverages GoRefactor's comprehensive lint system to identify design issues, structural problems, and maintainability concerns.

## Tool Features

### What It Detects

The tool identifies:

1. **Critical Issues** - File size violations that need immediate attention
2. **Design Smells**
   - **God Objects**: Structs with too many fields (>10)
   - **Large Classes**: Types with excessive members
   - **Data Clumps**: Parameter groups that should be extracted into types
   
3. **Code Organization Issues**
   - **Switch Statements**: Type-based switching patterns scattered across codebase (polymorphism anti-pattern)
   - **Excessive Parameters**: Functions with too many parameters (threshold: 6+)
   - **Unused Code**: Candidates for cleanup

4. **File Structure Issues**
   - Large files exceeding size limits (default: 300 lines)
   - Package organization problems
   - Import complexity

### Health Score Calculation

The tool calculates an overall health score (0-100) based on:
- **Error issues** (critical): -20 points each
- **Medium severity**: -3 points each  
- **Low severity**: -0.5 points each

Baseline: 100 points

## Usage

```bash
# Run analysis on current directory
./gorefactor-repo-check -dir .

# Output as JSON for processing
./gorefactor-repo-check -dir . -json

# Save to file
./gorefactor-repo-check -dir . -output report.txt
./gorefactor-repo-check -dir . -json -output report.json
```

## GoRefactor Repository Analysis Results

### Summary Statistics

| Metric | Value |
|--------|-------|
| **Health Score** | 55.5/100 ⚠️ |
| **Total Issues** | 135 |
| **Critical Issues** | 1 |
| **Medium Severity** | 2 |
| **Low Severity** | 37 |
| **Files Affected** | 68 out of 163 |
| **Most Common Issue** | Switch Statements (24 occurrences) |

### Critical Issues (1)

**File Size Violation**
- **File**: `analyzer/pattern_detector.go`
- **Problem**: 458 lines (limit: 300, over by 158 lines)
- **Impact**: Harder to test, understand, and maintain
- **Autofix Available**: `gorefactor split analyzer/pattern_detector.go --max 300`

### Code Smells Breakdown

| Smell Type | Count | Severity |
|------------|-------|----------|
| Switch Statements | 24 | Design Pattern Issue |
| Data Clumps | 8 | Low Priority |
| Excessive Parameters | 3 | Medium Priority |
| God Objects | 2 | High Priority |
| Large Classes | 2 | High Priority |

### Top Issues by Category

#### 1. Switch Statements (24 occurrences) - 🟠 Medium
**Pattern**: Type-based switching scattered across multiple files
**Files Affected**:
- `analyzer/call_graph.go` - 3 occurrences
- `analyzer/symbol_collect.go` - 2 occurrences
- `analyzer/symbol_walker.go` - 2 occurrences
- `analyzer/pattern_detector.go` - 3 occurrences
- And 14 more locations

**Impact**: Indicates missing abstraction or polymorphism opportunity. When the same type-switching pattern appears in multiple functions, it suggests:
- Missing interface or abstract type
- Need for method dispatch instead of type assertions
- Tight coupling to concrete types

**Recommendation**: Refactor to use interfaces and polymorphism instead of type switches.

#### 2. God Objects & Large Classes (4 occurrences total) - 🔴 High

**BlockInfo Struct** (`analyzer/analyzer.go`)
- **Problem**: 16 fields (threshold: >10)
- **Impact**: Violates Single Responsibility Principle, hard to test and extend
- **Suggestion**: Break into smaller, focused types grouping related fields

**TargetSpecification Struct** (`orchestrator/types.go`)
- **Problem**: 16 fields  
- **Impact**: Complex initialization, hard to validate and maintain
- **Suggestion**: Group fields into logical sub-types

#### 3. Excessive Parameters (3 occurrences) - 🟠 Medium

**Functions**:
- `extractBlocksFromFunc()` in `analyzer/cross_file_analyzer.go` (7 parameters)
- `chatPause()` in `cmd/gorefactor-agent/run_interactive_agentic_driver.go` (6 parameters)

**Impact**: Makes functions harder to use and test
**Recommendation**: Introduce parameter objects or builder patterns

#### 4. Data Clumps (8 occurrences) - 🟡 Low

**Pattern**: Parameter groups that appear together repeatedly
**Examples**:
- `[node *ast.File, functionName string, target string]` in multiple places
- `[filePath string, node *ast.File, position int]` in multiple places

**Recommendation**: Extract these into dedicated parameter types

### Most Affected Files

| Rank | File | Issue Count |
|------|------|------------|
| 1 | `cmd/gorefactor/cmd_find.go` | 7 |
| 2 | `cmd/gorefactor/cmd_split.go` | 6 |
| 3 | `analyzer/analyzer.go` | 6 |
| 4 | `analyzer/call_analyzer_advanced_test.go` | 4 |
| 5 | `cmd/gorefactor-agent/run_interactive_agentic_driver.go` | 4 |
| 6 | `analyzer/cross_file_analyzer.go` | 4 |
| 7 | `analyzer/call_graph.go` | 4 |
| 8 | `analyzer/pattern_detector.go` | 4 |

## Recommended Actions (Priority Order)

### 🚨 Critical - Do First
1. **Split `analyzer/pattern_detector.go`**
   - Command: `gorefactor split analyzer/pattern_detector.go --max 300`
   - Time estimate: 15-30 minutes
   - Payoff: Improves maintainability, reduces file complexity

### 🔴 High - Do Soon
2. **Refactor God Objects** 
   - `analyzer/analyzer.go` - Break BlockInfo into smaller types
   - `orchestrator/types.go` - Simplify TargetSpecification
   - Time estimate: 1-2 hours
   - Payoff: Easier to test, extend, and understand

3. **Replace Switch Statements with Polymorphism**
   - Create interfaces for type dispatch patterns
   - Time estimate: 2-4 hours
   - Payoff: Reduces coupling, improves extensibility

### 🟠 Medium - Plan Next
4. **Refactor Functions with Excessive Parameters**
   - Introduce parameter structs for the 3 functions identified
   - Time estimate: 30-45 minutes per function
   - Payoff: Better API, easier to test

## How to Use Reports

### Text Report
The text report (`REPO_HEALTH_REPORT.txt`) is human-readable and suitable for:
- Team discussions
- Printing for review
- Quick scanning of key issues
- Sharing via email/Slack

### JSON Report
The JSON report (`REPO_HEALTH_REPORT.json`) can be used for:
- Automated processing and trending
- Integration with CI/CD pipelines
- Historical comparison
- Building dashboards

## Comparison: Regular Linters vs. Code Smell Detector

| Issue Type | golangci-lint | gorefactor-repo-check |
|-----------|---|---|
| Syntax errors | ✅ | ❌ |
| Unused variables | ✅ | ❌ |
| **God Objects** | ❌ | ✅ |
| **Design Patterns** | ❌ | ✅ |
| **Code Smells** | ❌ | ✅ |
| **Complexity Analysis** | Partial | ✅ |
| **Refactoring Suggestions** | ❌ | ✅ |
| Formatting | ✅ | ❌ |

## Next Steps

1. **Immediate**: Run `gorefactor split analyzer/pattern_detector.go --max 300` to fix the critical issue
2. **Short-term**: Address the 2 God Objects affecting core data structures
3. **Medium-term**: Introduce polymorphism pattern to reduce switch statement duplication
4. **Ongoing**: Run this tool regularly (weekly/monthly) to track health trends

## Tool Architecture

The tool is built as a new CLI command that:

1. **Invokes** `./gorefactor lint` with `--json` flag
2. **Parses** the comprehensive output including:
   - File size violations
   - Code smells (design issues)
   - Structural problems
3. **Categorizes** issues by type and severity
4. **Calculates** a health score
5. **Generates** both human-readable and machine-readable reports
6. **Suggests** prioritized recommendations

The tool leverages GoRefactor's existing analysis infrastructure, making it:
- Fast (runs in seconds)
- Accurate (uses AST-based analysis)
- Actionable (includes autofix commands where applicable)

## Files Generated

1. **REPO_HEALTH_REPORT.txt** - Human-readable text report
2. **REPO_HEALTH_REPORT.json** - Machine-readable JSON report with full details
3. **gorefactor-repo-check** - Binary executable for running analyses

## Running on Other Repositories

To run this analysis on another Go repository:

```bash
cp gorefactor-repo-check /path/to/other/repo/
cd /path/to/other/repo
./gorefactor-repo-check -dir . -output health-report.txt
```

The tool analyzes the repository's codebase and generates reports identifying design issues that would take a human code reviewer significant time to find.
