# GoRefactor Improvement Plan

Observations from a real refactoring session against the gorefactor codebase itself.
Eight concrete improvements, ordered by implementation effort and impact.

## Implementation status — all shipped

| # | Improvement | Status |
|---|-------------|--------|
| 1 | MCP tool-surface parity | ✅ `format` now exposed; write surface was already present. `orchestrate` intentionally deferred (design-gated) |
| 2 | Name valid boundaries in extract errors | ✅ `noStatementsError` reports the nearest extractable range |
| 3 | Warn on suspiciously small extraction | ✅ `smallExtractionWarning` (<2 stmts or >40% smaller) |
| 4 | Nil-safe `lint --fix` error wrapping | ✅ already safe by construction (only wraps inside `if err != nil`); locked in with a `filepath.WalkDir` regression test |
| 5 | Suppress `high-blast-radius` infos by default | ✅ `--info` / `--verbose` flags + per-file collapse |
| 6 | Reduce duplicate-block false positives | ✅ min 3 stmts, built-in error-idiom deny-list, `lint.duplicate-ignore` config |
| 7 | `recommend --reduce-complexity` | ✅ greedy min-set-of-extractions to hit threshold |
| 8 | Explain `continue`/`break`/`return` barriers | ✅ `findJumpBarriers` names the barrier + suggests early-return restructuring (no `--scaffold` codegen; conservative first pass per the plan) |

---

## 1. MCP tool surface parity with the CLI — ✅ largely done

### Status (verified against the current tree)

This item's original premise is stale. `cmd/gorefactor/cmd_mcp_write.go` already
registers the full mutation surface behind `--allow-write`:

| Available via MCP (`--allow-write`) | Still not available via MCP |
|-------------------------------------|-----------------------------|
| `create`, `insert`, `extract`, `move`, `delete` | `format` |
| `rename`, `inline`, `replace-text`, `replace-body` | `orchestrate` |
| `change-signature`, `add-field`, `set-doc` | |
| `txn`, `undo` | |

The four priority mutations the plan originally called out — `insert`,
`replace-body`, `replace-text`, `rename` — are all present. Only `format` and
`orchestrate` remain unexposed.

### Remaining work

- `format` — trivial to add (in-process gofmt+goimports; no new design).
- `orchestrate` — deliberately deferred: it executes an arbitrary multi-op JSON
  plan, so exposing it over MCP needs a decision about how to bound/validate the
  plan under `--allow-write` rather than a mechanical schema addition.

### Files

- `cmd/gorefactor/cmd_mcp_schema.go` — tool schemas
- `cmd/gorefactor/cmd_mcp_write.go` — tool handlers

### Effort: Very low for `format`; `orchestrate` gated on a design decision, not effort.

---

## 2. `extract` failure messages should name valid boundaries

### Problem

Every failed extraction produces the same opaque error:

```
Error: no complete statements in lines X-Y
(must align with statement boundaries inside the function body)
```

The agent (or developer) must then manually inspect the file with `cat -n` and
guess where statement boundaries actually are — usually requiring 2–4 retries.

### Root cause

`gorefactor extract` validates the requested range against the AST but discards
the boundary information when rejecting. It knows which statements are at or near
the requested lines; it just doesn't report them.

### Solution

When extraction fails, return the nearest valid extractable range:

```
Error: lines 61-88 cannot be extracted — the range ends mid-if-block.
Nearest valid range starting at line 62: 62–91 (8 statements).
Note: range contains an early return; the caller will need restructuring.
Alternatively, try extracting the inner block: 63–89.
```

Implementation: after the boundary validation fails, scan forward and backward
from the requested lines to find the enclosing statement set, and include those
line numbers in the error.

### Files

- `cmd/gorefactor/cmd_extract_extract.go` — validation and error formatting
- `cmd/gorefactor/cmd_extract.go` — boundary detection logic

### Effort: Low (1 day)

---

## 3. `extract` should warn when the extracted block is suspiciously small

### Problem

When given a range that partially overlaps a statement (e.g. lines 91-110 where
the first clean statement begins at line 92), the extractor silently trims to the
nearest valid range — sometimes extracting only a single line. In one session:

```
gorefactor extract cmd/gorefactor/cmd_inline_inline.go 91 110 buildInlineEdits
→ Extracted buildInlineEdits (params=2, returns=1)
```

The actual extracted body was one assignment (`delStart := fset.Position(target.Pos()).Offset`),
returning a single `int`. The caller still had the remaining 18 lines of the
intended block inline. No warning was emitted.

### Solution

After a successful extraction, emit a warning if:
- The extracted function body contains fewer than 2 statements, **or**
- The extracted range is more than 40% smaller than the requested range

```
Warning: extracted buildInlineEdits contains only 1 statement (requested range was 20 lines).
Did you mean to include the surrounding block (lines 91–133)?
```

### Files

- `cmd/gorefactor/cmd_extract_extract.go` — post-extraction size check

### Effort: Very low (half a day)

---

## 4. `lint --fix` error wrapping must handle nil-able returns

### Problem

The `wrap-errors` autofix blindly rewrites `return x, err` to
`return x, fmt.Errorf("<ctx>: %w", err)`. When `err` can be `nil` at that point
(e.g. it is the return value of `filepath.WalkDir`, which returns nil on
success), the rewrite introduces a bug:

```go
// Before — correct
return files, err

// After autofix — broken: fmt.Errorf("...", nil) returns a non-nil error
return files, fmt.Errorf("walk: %w", err)
```

This broke 3 test packages in one session before being caught.

### Solution

Before applying the bare-wrap form, check whether `err` is provably non-nil at
the return point using a simple dataflow pass:

- If `err` is the direct result of a function call in the preceding statement
  (`err = f()`), it may be nil → use the guarded form.
- If `err` was assigned inside an `if err != nil` guard and is being returned
  from that guard's body, it is non-nil → the bare-wrap form is safe.

Emit the guarded form by default when uncertain:

```go
// Safe form (always correct):
if err != nil {
    return files, fmt.Errorf("walk: %w", err)
}
return files, nil
```

### Files

- `cmd/gorefactor/cmd_wrap_errors.go` — fix generation logic
- `cmd/gorefactor/cmd_wrap_errors_process.go` — nil-possibility analysis

### Effort: Medium (2–3 days)

This requires a conservative nil-possibility check. Starting with "always emit
the guarded form" as a safe default is acceptable; it is never wrong and the
guard can be optimized away later.

---

## 5. `lint` output: suppress `high-blast-radius` infos by default

### Problem

A `gorefactor lint` run on a medium-sized codebase produced 696 issues, of which
**448 (64%) were `[info] high-blast-radius`** entries. These are genuinely useful
for planning but they completely bury the `[warning]` items that require action.

A developer running `gorefactor lint` for the first time sees:

```
analyzer/walk.go [info] high-blast-radius: WalkGoFiles is load-bearing: 297 ...
analyzer/walk.go [info] high-blast-radius: ShouldSkipFile is load-bearing: 309 ...
... (400 more lines)
analyzer/walk.go [warning] error-not-wrapped: WalkGoFiles returns bare err ...
```

The actionable warning is invisible without scrolling past hundreds of info lines.

### Solution

Change the default output level to `warning` and above:

```bash
gorefactor lint .            # warnings only (current: all)
gorefactor lint . --info     # include info (blast-radius, untested-function)
gorefactor lint . --verbose  # everything
```

Additionally, collapse repeated blast-radius entries per file into a summary:

```
analyzer/walk.go [info] 6 high-blast-radius functions — run `gorefactor blast-radius analyzer/walk.go` for details
```

### Files

- `cmd/gorefactor/cmd_lint_lint.go` — output filtering
- `cmd/gorefactor/cmd_lint_options.go` — add `--info` / `--verbose` flags

### Effort: Low (1 day)

---

## 6. Duplicate-block detection: reduce false positives from idiomatic Go

### Problem

The duplicate-block checker flags `if err != nil { return err }` as a duplicate
in **102 places** at `[warning]` severity. This pattern is canonical idiomatic Go
and will never be refactored out. Similarly, 1-statement return-error blocks
appear in 35–44 places in many files.

This creates noise that trains users to ignore duplicate-block warnings entirely,
defeating the purpose of the check for real structural duplication (e.g. the
5-method `extractXxx` pattern in `diff_patterns.go`).

### Solution

**a) Raise the minimum extractable block size to 3 statements** (from 1).
One-statement blocks are almost always idiomatic and not extractable.

**b) Add a built-in exclusion for the common error-handling idioms:**

```go
// These patterns are excluded from duplicate-block by default:
if err != nil { return err }
if err != nil { return nil, err }
if err != nil { return 0, err }
if err != nil { return false, err }
```

Implemented as a normalized-form deny-list checked before hashing.

**c) Add `duplicate-ignore` patterns to `.gorefactor.yaml`:**

```yaml
lint:
  duplicate-ignore:
    - "if err != nil"      # normalized prefix match
    - "t.Fatal"            # test helper patterns
```

**d) Demote 1-stmt duplicates to `[low]`** (or `[info]` if `--info` is passed),
reserving `[warning]` for blocks of 3+ statements.

### Files

- `analyzer/cross_file_analyzer.go` — `extractBlocksFromFunc`, `FindDuplicateBlocks`
- `analyzer/cross_file_helpers.go` — `NormalizeCode`, `hashCode`
- `cmd/gorefactor/cmd_lint_duplicates.go` — severity assignment
- `config/config.go` — `duplicate-ignore` config field

### Effort: Medium (2–3 days)

---

## 7. `recommend` should target semantic chunks, not micro-blocks

### Problem

For a function with cyclomatic complexity 25, `recommend` suggested:

```
lines 42-44  complexity=1  stmts=3
lines 45-47  complexity=1  stmts=3
lines 49-53  complexity=3  stmts=13
```

These are tiny fragments. The real extraction opportunity — the 70-line for-loop
body — isn't surfaced because the scoring algorithm maximizes local complexity
reduction per extracted line, not overall parent function complexity reduction.

The result: an agent following `recommend` output makes 5 small extractions and
the parent function is still over threshold because it still contains 20 branches.

### Solution

Add a **complexity-threshold mode** to `recommend`: given a function over
threshold, find the minimum set of extractable sub-units that would bring the
parent below the threshold.

```bash
gorefactor recommend --reduce-complexity loop.go RunDriver
```

Output:

```
RunDriver (complexity 25, threshold 15) — needs to shed 10 complexity points.
Suggested extractions (reduces to complexity ~13):
  1. lines 72-91  "handleBudgetExhausted"  (-6 complexity, 4 branches)
  2. lines 121-138 "parsePlanFromRaw"      (-4 complexity, 3 branches)
```

The algorithm: build the complexity contribution of each top-level statement
block inside the function, sort by contribution, and greedily pick blocks until
the projected remainder is below threshold.

### Files

- `analyzer/analyzer.go` — new `RecommendComplexityReduction` function
- `cmd/gorefactor/cmd_recommend.go` — `--reduce-complexity` flag and output
- `analyzer/file_size_analyzer.go` — per-block complexity counting

### Effort: Medium-high (3–4 days)

---

## 8. `extract` should explain `continue`/`break`/`return` barriers

### Problem

When a block contains `continue`, `break`, or early `return` statements that
belong to an outer scope, extraction is impossible without restructuring the
caller. The extractor correctly rejects these but gives no guidance:

```
Error: no complete statements in lines 102-119
(must align with statement boundaries inside the function body)
```

In the session, the entire iteration body of `RunDriver` (the natural extraction
target) was blocked by `continue` statements. Without explanation, the agent
tried four different line ranges before giving up and leaving the function at
complexity 25.

### Solution

Detect the specific barrier and include it in the error with a concrete suggestion:

```
Error: lines 102-119 contain a `continue` statement that targets the enclosing
for-loop (line 71). Extraction would require restructuring the caller.

Suggested approach:
  Convert the `continue` branches to early returns from a helper:
    func (raw string, feedback string, ok bool) processRawResponse(raw string) (string, string, bool)
  Then the caller becomes:
    js, feedback, ok := processRawResponse(raw)
    if !ok { continue }

Run `gorefactor extract loop.go 102 119 processRawResponse --scaffold` to
generate the restructured helper signature.
```

A `--scaffold` flag would generate the helper stub and the updated call site
without actually writing files, so the developer can review and complete it.

### Files

- `cmd/gorefactor/cmd_extract_extract.go` — barrier detection
- `cmd/gorefactor/cmd_extract.go` — barrier classification and message generation
- `cmd/gorefactor/cmd_extract_analyze.go` — new `--scaffold` mode

### Effort: High (4–5 days)

The scaffold generation requires understanding what variables flow in/out across
the `continue` branches, which is the hardest part of the extract refactoring
problem. A conservative first pass could just name the barrier and skip the
scaffold suggestion.

---

## Priority and Roadmap

### Wave 1 — Quick wins (< 1 week total)

| # | Improvement | Effort |
|---|-------------|--------|
| 3 | Warn when extraction is suspiciously small | 0.5 days |
| 5 | Suppress `high-blast-radius` infos by default | 1 day |
| 2 | Name valid boundaries in extract errors | 1 day |
| 6a | Raise minimum duplicate-block size to 3 stmts | 0.5 days |

Wave 1 delivers the most visible quality-of-life improvements with the least
risk. Items 2 and 3 fix daily friction for anyone using `gorefactor extract`
interactively.

### Wave 2 — Medium lifts (1–2 weeks total)

| # | Improvement | Effort |
|---|-------------|--------|
| 1 | MCP parity — ✅ done except `format` (trivial) and `orchestrate` (design-gated) | ~0 |
| 4 | `lint --fix` nil-safe error wrapping | 2–3 days |
| 6b/c/d | Duplicate-block ignore patterns + severity tuning | 2 days |

The MCP server is already production-ready for agents doing full refactoring
sessions — the write surface (`insert`/`replace-*`/`rename`/`inline`/etc.) landed
ahead of this plan, so Wave 2 is really just items 4 and 6b/c/d.

### Wave 3 — Deeper features (2–4 weeks)

| # | Improvement | Effort |
|---|-------------|--------|
| 7 | `recommend --reduce-complexity` mode | 3–4 days |
| 8 | `continue`/`break` barrier explanation + `--scaffold` | 4–5 days |

Wave 3 items are research-adjacent — they require dataflow analysis and are
valuable but not blocking anything today.

---

## Cross-cutting notes

**Testing for improvements 2, 3, 8:** the extract command already has good test
coverage in `cmd/gorefactor/cmd_extract_extract_test.go`. New test cases should
cover: (a) ranges that clip at statement boundaries, (b) ranges containing jump
statements, (c) the small-extraction warning threshold.

**Testing for improvement 4:** add a test fixture where `err` is the return of
`filepath.Walk` (nil on success) and verify the autofix emits the guarded form,
not the bare-wrap form.

**Config compatibility:** improvements 6c (duplicate-ignore) and any new lint
flags should be additive — existing `.gorefactor.yaml` files must continue to
work unchanged.

**MCP versioning (improvement 1):** new mutation tools should be gated behind a
server capability flag so MCP clients that only want read-only analysis don't
accidentally get write tools. The existing `--allow-write` mechanism handles this.
