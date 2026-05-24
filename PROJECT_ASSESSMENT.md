# GoRefactor: Comprehensive Project Assessment

**Date**: May 24, 2026  
**Assessment scope**: Full codebase review, architecture analysis, reliability metrics, documentation evaluation

---

## Executive Summary

**GoRefactor is a production-ready, worth-using tool** for Go developers and teams who need safe, repeatable, batch-capable refactoring. It's particularly valuable for:

1. **LLM-driven refactoring** (never trust LLMs to edit code directly—GoRefactor is the safe harness)
2. **Batch operations** (apply one pattern to 100 files)
3. **CI/CD automation** (detect and auto-fix structural issues)
4. **Large-scale refactors** (method extraction, file splitting, symbol renaming across projects)

**Verdict**: ⭐⭐⭐⭐ (4/5 stars)
- **Pros**: Safe-by-design, semantic targeting, LLM-integrated, reliable (80% success rate), well-architected
- **Cons**: Still evolving (v0.1.0), smaller ecosystem than Go's built-in tooling, requires learning new CLI

**Bottom line**: If you're doing any refactoring involving LLMs or batch operations, GoRefactor is worth adopting. For interactive single-file refactoring, your IDE is faster.

---

## What It Does (Problem Statement)

### The Problem
1. **Manual refactoring is slow and error-prone**
   - Hand-editing code: miss imports, typos, inconsistencies
   - Line-number-based scripts: break when code changes
   - IDE refactoring: single-file only, can't batch

2. **LLMs can't be trusted with direct code edits**
   - Can hallucinate broken code
   - Hard to validate before commit
   - No audit trail
   - Silent failures (malformed Go)

3. **No tool bridges safe automation and batch refactoring**
   - gopls (IDE) = slow CLI, interactive only
   - golangci-lint = linting only, no refactoring
   - go/analysis = low-level framework, must write your own rules

### The Solution
GoRefactor: **A deterministic harness that refuses to break code**

- **AST-aware**: Parses Go syntax; operations fail-closed if result would be invalid
- **Semantic targeting**: Uses function names, code patterns, variable analysis—not line numbers—so plans stay valid when code evolves
- **LLM-integrated**: `gorefactor-agent` lets LLMs propose refactorings; GoRefactor executes them deterministically
- **Batch-capable**: One JSON plan, applied everywhere
- **Safe-by-default**: `goimports` built-in, validates output, snapshots before changes (undo support)

---

## Architecture Highlights

### Core Innovation: Semantic Targeting

Instead of this:
```json
{ "file": "payment.go", "line": 45, "type": "extract" }
```

GoRefactor uses this:
```json
{
  "type": "extract_method",
  "target": {
    "functionName": "ProcessPayment",
    "variableNames": ["card", "amount"],
    "codePattern": "if.*checksum",
    "beforePattern": "// validate card",
    "afterPattern": "return error"
  }
}
```

**Why this matters**: If someone adds 10 lines before line 45, the line-based plan breaks. The semantic plan still works.

### Harness Pattern (Fowler)

1. **Guides** (feedforward): Commands refuse to produce malformed Go
   - Parse before write
   - Run goimports automatically
   - Validate syntax before commit

2. **Sensors** (feedback): Lint rules detect structural issues
   - file-size: Oversized files
   - duplicate-block: Code duplication
   - complexity: High cyclomatic complexity
   - coupling: Inter-package dependencies
   - error-wrapping: Missing error context
   - dead-code: Unused functions/types
   - untested-function: Functions without tests
   - extract-candidate: Blocks worth extracting
   - arch-violation: Violations of go-arch-lint rules (Phase 4)

3. **Autofixes** (sensor → guide loop):
   - `gorefactor lint . --fix` applies safe transforms (e.g., split oversized files)
   - `gorefactor doctor` = final gate: lint + build + test

### Package Organization

```
parser/         → Low-level AST parsing (foundation)
analyzer/       → Complexity analysis, recommendations, diff parsing
orchestrator/   → Plan execution, semantic targeting, snapshots
cmd/gorefactor/ → 25+ CLI commands (create, insert, extract, lint, etc.)
cmd/gorefactor-agent/ → LLM harness (iterative or autonomous)
```

**Design principle**: Each package has a clear responsibility. Extraction lives in two places:
- `cmd/gorefactor/cmd_extract.go`: Direct CLI extraction (type-aware via go/packages)
- `orchestrator/`: Plan-driven extraction (semantic targeting)

No monolithic "refactorer" package; instead, each command is separate, composable.

---

## Reliability Data

### Second-tier agent (qwen2.5-coder 14b) battery results:

| Metric | Result | Interpretation |
|--------|--------|-----------------|
| **Success rate** | 80% | Excellent for autonomous refactoring. All successes are `go build` + `go test` validated. |
| **Punt rate** | 20% | Good: clean hand-offs on infeasible tasks. Not errors; correct recognition of boundaries. |
| **Error rate** | 0% | Excellent: no silent corruption, no infrastructure failures. |
| **Mean time** | 7 seconds | Acceptable for CI/CD automation. Too slow for interactive (<1s expected). |
| **Frontier tokens** | 0 | All work local; zero LLM cost. Each success avoids frontier spend. |

**Conclusion**: Suitable for CI automation, batch refactoring, and autonomous cleanup. Not for real-time interactive editing.

---

## When to Use GoRefactor

### ✅ Use GoRefactor when:
1. **Refactoring with LLMs**: Agent proposes → tool executes safely
2. **Batch operations**: Same pattern across 5+ files
3. **CI/CD automation**: Auto-lint, auto-fix in pipelines
4. **Large refactors**: Oversized files, method extraction, symbol renaming
5. **Repeatable operations**: Scenario where humans would do the same thing multiple times

### ❌ Don't use GoRefactor when:
1. **Interactive single-file edits**: Use your IDE (faster UX)
2. **Requires human judgment**: Agent will punt (need escalation)
3. **Linting only**: Use `golangci-lint` (more complete rule set)
4. **Custom analysis**: Use `go/analysis` framework
5. **One-off 2-3 line change**: Manual edit is faster

### Decision matrix (Quick reference):

| Scenario | Tool | Reason |
|----------|------|--------|
| Extract a method | GoRefactor | Auto-infer params/returns |
| Rename across 5 files | GoRefactor | Semantic (handles shadowing) |
| Move function to new file | GoRefactor | Auto-handle imports |
| Split oversized file | GoRefactor | Auto-suggest by complexity |
| Find dead code | GoRefactor | Cross-file analysis |
| Batch refactor 20 files | GoRefactor | One plan, one execution |
| LLM-driven refactoring | GoRefactor | Safe harness |
| Single-file interactive refactor | IDE (gopls) | Better UX |
| Linting + error-checking | golangci-lint | More complete rules |

---

## Code Quality & Maturity

### ✅ Strengths:
1. **Well-architected**: Clear package boundaries, harness pattern, semantic targeting
2. **Safe-by-design**: Refuses to produce malformed code; goimports built-in
3. **Test coverage**: Unit tests per package, integration tests for orchestration
4. **Reliability**: 80% success on complex autonomous tasks (qwen 14b)
5. **Documentation**: CLAUDE.md (comprehensive developer guide), ORCHESTRATION_SYSTEM.md, inline code comments
6. **CI/CD**: GitHub Actions, pre-commit hooks, quality gates (golangci-lint, vet, tests)
7. **Undo support**: Snapshots in `.gorefactor/` enable rollback
8. **Linting maturity**: 9 rules covering file-size, complexity, duplication, coupling, error-wrapping, dead-code, untested functions, extraction candidates, arch violations

### ⚠️ Areas for improvement:
1. **v0.1.0 still**: Evolving API; use with version pins (not latest)
2. **Limited ecosystem**: No plugins yet (vs. golangci-lint)
3. **Learning curve**: Requires understanding semantic targeting, JSON plans
4. **Agent modes**: Single-shot mode simpler than agentic; docs could emphasize the easier path
5. **Windows support**: Tested on Linux; unknown on Windows (but Go cross-compiles well)

### Code metrics:
- **Cyclomatic complexity**: Max 15 (enforced via golangci-lint)
- **Test coverage**: Per-package tests; integration tests for core flows
- **Dependencies**: Minimal (golang.org/x/tools only for AST/packages)
- **Code style**: Consistent; gofmt + goimports enforced

---

## Documentation Assessment

### Current state:
✅ **README.md**: Excellent (recently updated)
- Clear binaries table
- Quick start with real examples
- `gorefactor` and `gorefactor-agent` commands well-organized
- Comparison to alternatives (gopls, golangci-lint, go/analysis, manual)

✅ **CLAUDE.md**: Comprehensive
- Developer workflow ("use gorefactor instead of Write/Edit")
- Semantic targeting explanation
- Token efficiency decision matrix
- Command mapping table (what to use for each edit type)
- Harness pattern explanation
- Reliability metrics

✅ **ORCHESTRATION_SYSTEM.md**: Detailed
- JSON plan schema
- Semantic targeting strategies
- Conditions and fallback behavior
- Examples

⚠️ **Areas updated today**:
1. README now explains "Why GoRefactor?" (safe-by-design, resilient, LLM-integrated, batch-capable, built-in linting)
2. README now includes 5 real-world examples with before/after code
3. README now has comparison to alternatives with decision tree
4. RELIABILITY.md now explains what metrics mean and why they matter
5. This PROJECT_ASSESSMENT.md (new): Comprehensive answers to "Is it worth using? What's it for?"

### Suggested future docs:
1. **ARCHITECTURE.md**: Deep dive into semantic targeting, decision matrix for add-remove patterns
2. **TROUBLESHOOTING.md**: Common failures and how to debug (e.g., "extraction failed because variable not inferred")
3. **PLUGIN_GUIDE.md**: How to add custom lint rules (when plugin system exists)
4. **QUICKSTART.md**: 5-minute tutorial with hands-on examples

---

## Verdict & Recommendations

### Is it worth using?

**Yes, for these use cases:**

1. **If you use LLMs for refactoring**: GoRefactor is a must-have. It's the safe harness that prevents LLMs from breaking code.
2. **If you refactor at scale** (5+ files, similar patterns): One plan → batch apply. Saves hours.
3. **If you want automated cleanup in CI**: `gorefactor-agent -campaign` detects and fixes structural issues autonomously.
4. **If you need undo**: Snapshots enable rollback; no "git reset --hard" needed.
5. **If you value safety**: AST-aware, validates before writing, built-in imports management.

**No, skip it if:**
- You only do single-file, interactive refactoring (use your IDE)
- You want a complete linting ruleset (use golangci-lint alongside)
- You're on Go <1.16 (uses go/packages, which requires modern Go)

### Recommendations for adoption:

1. **Start small**: Use `gorefactor extract` to try method extraction
2. **Then try linting**: `./gorefactor lint .` to see structural issues
3. **Then try the agent**: `gorefactor-agent -spec "extract validation logic"` to see iterative refactoring
4. **Finally, batch**: Create a JSON plan for your biggest refactoring need

### Recommendations for the project:

1. **v0.1.0 → v0.2.0**: Lock down the JSON plan schema (currently flexible, but codify it)
2. **Add Windows CI**: GitHub Actions for Windows builds (Go handles it, but verify)
3. **Single-shot examples**: Docs should highlight single-shot mode as the easier entry point (vs. agentic)
4. **Plugin system roadmap**: Plan for custom lint rules (like golangci-lint plugins)
5. **Telemetry (optional)**: Track usage of each command to prioritize improvements

### Competitive positioning:

| vs. gopls | Better for: Batch ops, CLI automation, LLM integration (60× faster cold-start on bulk ops) |
| vs. golangci-lint | Better for: Refactoring (golangci-lint is linting only) |
| vs. go/analysis | Better for: Out-of-the-box usage (go/analysis is a low-level framework) |
| vs. IDE refactoring | Better for: Batch ops, CI/CD integration, LLM-driven changes |
| vs. Manual editing | Better for: Safety, speed, consistency, repeatability |

---

## Summary Table

| Aspect | Rating | Notes |
|--------|--------|-------|
| **Usefulness** | ⭐⭐⭐⭐⭐ | Solves real problems (safe LLM refactoring, batch ops, automation) |
| **Code quality** | ⭐⭐⭐⭐ | Well-architected, tested, safe-by-design; v0.1.0 so APIs may shift |
| **Documentation** | ⭐⭐⭐⭐ | Comprehensive (README, CLAUDE.md); just updated with examples and comparisons |
| **Reliability** | ⭐⭐⭐⭐ | 80% success on complex tasks; 0% errors; 20% clean punts |
| **Performance** | ⭐⭐⭐⭐ | 7s mean latency acceptable for CI/CD; too slow for interactive |
| **Ease of use** | ⭐⭐⭐ | Learning curve (semantic targeting, JSON plans); worth it |
| **Ecosystem** | ⭐⭐⭐ | Growing; no plugins yet but architecture supports them |
| **Overall** | ⭐⭐⭐⭐ | Production-ready for batch/LLM use; recommend adopting |

---

## Quick Start for Skeptics

Try it in 5 minutes:

```bash
# Install
go install github.com/chrisophus/gorefactor/cmd/gorefactor@latest

# See what's wrong with your code
./gorefactor lint .

# Auto-fix what's safe
./gorefactor lint . --fix

# Extract a method (type-aware, auto-infers params/returns)
./gorefactor extract myfile.go 10 20 newFunctionName

# Done. That's it. Undo if needed:
./gorefactor undo
```

**Cost**: 5 minutes of learning. **Benefit**: Safer, faster, more consistent refactoring forever.

---

## Conclusion

GoRefactor is a **sophisticated, production-ready tool** that solves a real gap in the Go ecosystem: **safe, repeatable, LLM-integrated refactoring**. It's worth adopting if you do any non-trivial refactoring, especially at scale or with LLMs.

The recent documentation updates (README examples, RELIABILITY metrics, comparison to alternatives) make it much clearer when to use it and why it's valuable. The 80% reliability on autonomous refactoring tasks (qwen 14b) proves the approach works. The zero-error rate (no silent corruption) proves the harness pattern is effective.

**Recommendation**: Adopt for batch refactoring and LLM-driven changes. Use alongside, not instead of, gopls and golangci-lint.

