# GoRefactor Improvements Inspired by FastContext

**Source**: https://github.com/microsoft/fastcontext  
**Paper**: https://arxiv.org/abs/2606.14066  
**Date**: 2026-06-21

---

## What FastContext Does

FastContext's core insight: **the same model shouldn't explore the repo AND make edits in the same context window**. Exploration is cheap, parallel, and read-only. Editing requires focused reasoning. Mixing them pollutes both.

The architecture:
- Explorer subagent gets a natural-language query, issues **parallel** Read/Glob/Grep calls, returns compact `file:line` citations
- Main agent gets those citations as focused evidence, then edits
- Explorer never mutates anything
- All output is aggressively capped (100 lines for grep, 2000 for read)
- Results: **60.3% fewer main-agent tokens**, **+5.5 points** on SWE-bench

---

## GoRefactor's Gap

GoRefactor's agent does exploration + planning + mutation in one role, one context. In `loop.go`: the agent builds a code map, does sense calls, generates a plan, applies it — all in a single loop. No separation, no parallelism in the sense phase, and output caps exist (scattered `trim()` calls) but aren't systematic.

---

## Planned Improvements

### 1. `gorefactor context <query>` — FastContext-style exploration command

**Priority**: Very High  
**Effort**: Medium  
**Impact**: Direct token savings on every exploration-heavy task

The single biggest win. Right now if an LLM wants to know "what calls ProcessRequest?" it must read files or ask the user. A dedicated read-only exploration command lets it delegate that question cheaply.

```bash
gorefactor context "where is authentication handled?"
# → handlers.go:42-58 (AuthMiddleware)
# → middleware.go:15-31 (TokenValidator)

gorefactor context "what would break if I rename ParseRequest?"
# → cmd/gorefactor/cmd_move.go:87 (calls ParseRequest)
# → parser/parser.go:203 (calls ParseRequest)
# → parser/parser_test.go:44 (calls ParseRequest)
```

Internally runs `find-callers`, `find-uses`, `inspect` — **in parallel** — returning only file:line citations. The main LLM doesn't read those files; it just knows where to look.

This is a direct port of FastContext's contract: *find the relevant code; main agent uses that focused evidence to edit*.

**Implementation sketch**:
- New command: `cmd/gorefactor/cmd_context.go`
- Accepts natural language query
- Dispatches parallel goroutines for: find-callers, find-uses, inspect, grep
- Returns `<final_answer>` block with file:line citations
- Hard cap: 20 citations max

**Output format**:
```
handlers.go:42-58     AuthMiddleware (matches: "authentication")
middleware.go:15-31   TokenValidator (matches: "authentication")
```

---

### 2. Parallel pre-flight validation before mutations

**Priority**: High  
**Effort**: Medium  
**Impact**: Fewer error cycles — LLM sees all blockers at once

FastContext issues multiple tool calls simultaneously. GoRefactor validates sequentially — parse file, find target, check callers, check imports — stopping at the first failure. An LLM may get three separate error cycles for one bad move.

Instead, before any mutation, run all checks in parallel and return one consolidated report:

```json
{
  "preflight": {
    "targetExists": true,
    "callers": ["handlers.go:87", "main.go:12"],
    "importCycle": false,
    "typeConflicts": []
  },
  "safe": false,
  "blockers": ["has 2 callers — remove them first or use --force"]
}
```

LLM sees everything at once and plans a complete recovery instead of playing whack-a-mole.

**Implementation sketch**:
- Add `--preflight` flag to move, delete, extract
- Run target validation + caller analysis + import graph check concurrently via goroutines
- Return consolidated JSON before mutating
- If blockers exist: exit without mutation, return preflight report
- If clean: proceed with mutation

---

### 3. Systematic output capping

**Priority**: Medium  
**Effort**: Low  
**Implementation**: Straightforward

FastContext enforces hard limits on every tool: Grep caps at 100 lines, Read caps at 2000. GoRefactor has scattered `trim()` calls in the agent's sense tools but no consistent policy. `find-callers` on a popular function could return hundreds of results and flood the context.

**Changes needed**:
- Add `--limit N` flag to all analysis commands (find-callers, find-uses, find-implementations, recommend)
- Default limits: find-callers=50, find-uses=50, recommend=10, inspect=2000 chars
- When truncated, output: `(truncated — use --limit N or refine query)`
- Update agent system prompt: "use --limit liberally; truncated evidence is better than no answer"

FastContext's key insight: *truncated evidence is better than no answer* — the agent can always ask again more specifically.

---

### 4. `--explore` mode for the agent

**Priority**: High  
**Effort**: High  
**Impact**: Separation of concerns — mirrors FastContext architecture exactly

`gorefactor-agent` currently does exploration → planning → mutation in one loop. FastContext shows exploration should be a separate, cheaper, faster subagent.

A `--explore` flag that:
- Runs only sense tools (inspect, find-callers, skeleton, recommend)
- Never invokes any mutation operations
- Returns compact file:line citations
- Hard turn budget: 3-4 turns max (like FastContext's `--max-turns`)

```bash
gorefactor-agent --explore "where should I add rate limiting?"
# → middleware.go:15-45  (current request pipeline)
# → handlers.go:88-102  (entry points to protect)
```

Main LLM uses this output to plan, then calls `gorefactor-agent` again for the actual mutation. Two small focused calls instead of one large tangled one.

**Implementation sketch**:
- Add `--explore` flag to `RunDriver` config
- When set: system prompt switches to exploration-only variant
- allowedOps filtered to [] (no mutations allowed)
- Output format: `<final_answer>` block with file:line citations
- Return after first non-tool-call response (no plan/apply phase)

---

### 5. JSONL session trajectory logging

**Priority**: Medium now, High later  
**Effort**: Low  
**Impact**: Debugging now; training data for smaller specialist models later

FastContext records every tool call as a JSONL trajectory file. GoRefactor has an undo journal (per-operation) but no session-level record of exploration + reasoning + mutation.

This matters for two reasons:
1. **Debugging**: what did the agent actually do across a session?
2. **Training data**: successful refactoring trajectories can train smaller specialist models — exactly how FastContext produced 4B models that outperform prompted large models

**Format**:
```jsonl
{"session":"abc123","turn":1,"tool":"inspect","args":{"file":"handlers.go"},"result_chars":842,"ms":45}
{"session":"abc123","turn":2,"tool":"find-callers","args":{"target":"ParseRequest"},"result_chars":210,"ms":120}
{"session":"abc123","turn":3,"op":"move","args":{"from":"handlers.go","target":"ParseRequest","to":"router.go"},"success":true,"ms":890}
```

**Implementation sketch**:
- Add `--traj <file>` flag to `gorefactor-agent` (default: `.gorefactor/trajectory.jsonl`)
- Each tool call and mutation appends one JSON line
- Trajectory survives across `--max-iter` retries (full session, not just last attempt)
- `gorefactor history --json` already exists for undo journal; trajectory is additive

---

## Priority Order

| # | Improvement | Files | Effort | Token Impact |
|---|-------------|-------|--------|--------------|
| 1 | `gorefactor context <query>` | `cmd_context.go` (new) | Medium | Very High |
| 2 | Parallel pre-flight validation | `cmd_move.go`, `cmd_direct.go` | Medium | High |
| 3 | Systematic output capping | All analysis cmds + agent prompt | Low | Medium |
| 4 | `--explore` mode for agent | `loop.go`, `prompt.go` | High | High |
| 5 | JSONL trajectory logging | `loop.go`, agent main | Low | Medium (future: High) |

---

## FastContext Results We're Targeting

| FastContext Result | GoRefactor Equivalent |
|---|---|
| 60.3% fewer main-agent tokens | Context command offloads exploration |
| +5.5 benchmark points | Fewer failed attempts via pre-flight |
| 4B model matches 30B for exploration | Trajectory data enables specialist model |

---

## Related Work Already Done

- Phase 1-4: Error context system (structured errors + recovery suggestions)
- This plan is the next step: reduce exploration cost, not just recovery cost
- Together: lower token cost both before mutations (exploration) and after failures (recovery)
