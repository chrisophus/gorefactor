# Benchmark Findings

## Before/After: improvements shipped in this session

| Scenario | Before tokens | After tokens | Before outcome | After outcome |
|---|---|---|---|---|
| Rename (cross-file) | 27,328 | — | ✓ fixed | unchanged |
| Move function to new file | 73,424 | 36,454 | ✗ punt | ✓ fixed |
| Find callers (analysis) | 20,889 | 13,685 | ✗ punt | ✓ fixed |

**Root causes fixed:**
- `move_function` tool added — agent no longer falls back to create+delete for top-level functions
- `report` tool added — analysis tasks can return answers without triggering go build+test
- System prompt updated: "act first on clear tasks", explicit tool choice rules for move/delete/analysis
- `recommend --short` added: 404 chars vs 10,524 (26x smaller, now 17x better than reading the file)
- `delete --safe` added: checks callers before deleting, preventing silent build breaks
- Anthropic provider now tracks `input_tokens`/`output_tokens` from response body
- Agent gate (`runIn`) now passes `GOTOOLCHAIN=auto` and `GOTMPDIR` to avoid noexec issues



## Three-way comparison: "rename emitRunMetrics → emitAgentMetrics"

| Approach | Tokens | Time | Outcome |
|---|---|---|---|
| `./gorefactor rename` (direct CLI) | ~0 | 33ms | ✓ success |
| gorefactor-agent (claude-haiku-4.5) | 27,328 | ~45s | ✓ success |
| Direct Edit/Write (estimated) | 8,000–16,000 | — | — |

The direct CLI call costs essentially zero tokens. The agent burns 27K tokens reaching the
same result. Direct file editing (if Claude Code did it without gorefactor) would cost
8–16K tokens just to read the three affected files.

## Live agent run results

| Scenario | Tokens | Steps | Outcome | Root cause of failure |
|---|---|---|---|---|
| Rename (cross-file) | 27,328 | 6 | ✓ fixed | — |
| Move function to new file | 73,424 | 12 | ✗ punt | `remove_code_block` needs `location` param the model didn't provide |
| Find callers (analysis) | 20,889 | 5 | ✗ punt | Agent explored but couldn't synthesise answer with available tools |

## Static context-size analysis

These ratios measure bytes LLM must read+write (direct approach) vs
bytes to invoke the gorefactor command + receive output.

| Command | Savings ratio |
|---|---|
| `lint .` | 770x |
| `find-uses` | 259x |
| `find-implementations` | 222x |
| `find-callers` | 156x |
| `rename` | 135x |
| `inspect` (single file) | 7x |
| `list-functions` | 24x |
| **`recommend`** | **0.7x (worse than reading the file)** |

## Decision guide

### Use direct `./gorefactor` CLI (0 tokens, instant)
Best choice when the task is well-scoped and structural:
- rename, delete, move, insert, replace
- find-callers, find-uses, find-implementations (analysis queries)
- lint, inspect, list-functions (structural summaries)
- **Never use `recommend` as a context saver — it produces more bytes than it saves**

### Use gorefactor-agent (20–75K tokens per task)
Worth it when:
- The task is open-ended or multi-step and you'd otherwise spend frontier tokens iterating
- Campaign mode (`-campaign`) for autonomous cleanup from lint findings
- The task requires discovery before action (unknown callers, unknown file structure)

Not worth it when:
- You already know the exact operation (just call the CLI directly)
- The task is pure analysis (use `find-callers` / `find-uses` directly)

The agent is tool-calling only (default agentic loop, or `-campaign`). There is no single-shot plan mode and no interactive pause mode — use the direct CLI or an IDE for those workflows.

### Use Claude Code direct edit (for logic changes)
When the change requires semantic understanding:
- Bug fixes, algorithm rewrites, architectural changes
- Error handling, new business logic
- Type changes with semantic implications



## 2026-07-19 — Campaign cost-of-pass (junior vs frontier)

Method: `go run ./benchmark -agent-corpus-run -models claude-haiku-4-5,claude-sonnet-4-6 -modes agentic`
against the 9-task agent corpus (easy/medium/hard). Agentic mode is the remaining
product harness after campaign-or-cut (single-shot/interactive removed). List prices
from `benchmark/pricing.go` (early-2026 rates; ranking input, not an invoice).

| model | mode | pass | cost | cost/pass | tok/pass | avg_ms |
|---|---|--:|--:|--:|--:|--:|
| claude-haiku-4-5 | agentic | 9/9 | $0.3177 | $0.3177 | 300596 | 13180 |
| claude-sonnet-4-6 | agentic | 9/9 | $0.5579 | $0.5579 | 175658 | 12891 |

**Decision (keep campaign):** junior Haiku matched Sonnet at 100% on every solvable
corpus class and was **~1.75× cheaper per pass** ($0.32 vs $0.56). Keep
`gorefactor-agent` campaign/agentic as the headline LLM surface; escalate to frontier
only when the junior stalls. Direct `./gorefactor` CLI remains strictly cheaper for
scoped structural tasks (see tables above).

## Gaps identified (status)

1. **`safe_delete`** — ✅ done (`delete --safe`, benchmarked 76x).
2. **`move_with_delete`** — ✅ done. `move_function` is now in the agent
   tool catalog *and* the source file is resolved deterministically (see
   2026-05-19 below). `move_method`/`move_function` were dispatchable but
   unadvertised — the root cause of the original move punt.
3. **Better `remove_code_block` error message** — ⏳ open (low priority;
   not exercised by the battery's task classes).
4. **`recommend` output trimming** — ✅ done (`recommend --short`,
   benchmarked 15x; was 0.7x).
5. **Anthropic provider token tracking** — ✅ done.

## 2026-05-19 — data-driven loop via the live reliability battery

Method: controlled before/after on a fixed target commit, varying only
the agent binary; local junior `qwen2.5-coder:14b`, 3 runs/class. The
junior is deterministic per binary+commit, so deltas are causal.

| class | before | after | mean steps | mean secs | what fixed it |
|---|--:|--:|--:|--:|---|
| scaffold | 100% | 100% | 2.0 | 8 | no regression (3→2 steps) |
| rename | **0%** | **100%** | 3.0 | 9 | explicit `function/method/type` param descriptions + "name the identifier, never guess the file" prompt rule |
| movefunc | **0%** | **100%** | 2.0 | 6 | `move_function` added to catalog + `resolveSymbolFile` (deterministic source-file resolution) + accurate "moved X from A to B" result message |
| analysis | 100% | 100% | 2.0 | 8 | measurement artifact — `finish` on an unchanged repo passes the gate, so this class can't discriminate; `report` is the real mechanism |
| infeasible | punt | punt | 1.0 | 3 | correct outcome |
| **all** | **40%** | **80%** | — | 7 | every *solvable* class now 100%; the only punts are the task that should punt |

Root-cause chain (each step found by one `-verbose` trace, not the
aggregate table):

1. `rename` punted: junior omitted the required `function` arg (the
   `"or"` param descriptions were uninformative to a 14B model) and
   guessed the wrong file. Fixed via explicit descriptions + prompt rule.
2. `move_function`/`report` were wired in `dispatch_tool.go` +
   `apply_op.go` but **absent from `toolCatalog()`** — invisible to the
   model. Added both.
3. `movefunc` still punted: the junior names the symbol reliably but
   guesses its file, and `move_function` is file-scoped. Made the tool
   resolve the symbol's real file itself (`resolveSymbolFile`) — no LLM
   retry. (Deterministic per the user's steer: don't rely on the LLM.)
4. `movefunc` still punted: the move *succeeded* but the result string
   reported the source file, so the junior thought it went to the wrong
   place and false-punted (which rolled the move back). Made the
   success message op-aware ("moved X from A to B").

**Headline sensor finding:** the battery (worktree + `git clean`) acted
as a sensor and surfaced a real repo defect — `.gitignore`'s unanchored
`gorefactor` pattern matched the `cmd/gorefactor/` *source dir*, so
every untracked file there was gitignored (invisible to `git status`,
unstaged by `git add .`, immune to `git clean`). A stray
`case_convert.go` from a movefunc run got stuck, duplicated a symbol,
broke `go build` in the worktree, and cascaded every gate-dependent
class to punt while `report`-based `analysis` stayed green — the
diagnostic signature for "broken gate, not broken agent". Fixed by
anchoring the pattern to `/gorefactor`.

**Wall-clock added:** the battery report (generated locally by `scripts/reliability.sh`, not committed) now reports `mean secs` (the real
adoption gate — the junior is free in frontier tokens, not in time).
At ~7s/run on this hardware, time is not a constraint at this scale.

Regenerate the live table locally with
`scripts/reliability.sh [iters]`.
