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
- Campaign mode for autonomous cleanup (no human in the loop)
- The task requires discovery before action (unknown callers, unknown file structure)

Not worth it when:
- You already know the exact operation (just call the CLI directly)
- The task is pure analysis (use `find-callers` / `find-uses` directly)

### Use Claude Code direct edit (for logic changes)
When the change requires semantic understanding:
- Bug fixes, algorithm rewrites, architectural changes
- Error handling, new business logic
- Type changes with semantic implications

## Gaps identified (new workflows to support)

1. **`safe_delete`**: `find-callers X` → if callers > 0, refuse; else `delete`
   - Prevents broken builds when deleting functions that have callers

2. **`move_with_delete`**: atomic "extract function to new file" compound command
   - Agent failed on move because `remove_code_block` needed a location param
   - A single `move` command already exists — but the agent didn't use it

3. **Better `remove_code_block` error message**: show the correct `location` format on failure

4. **`recommend` output trimming**: emit top-3 candidates as one-line summaries instead of full JSON

5. **Anthropic provider token tracking**: currently emits 0 for prompt/completion tokens
   when using `-provider anthropic` — add the usage parsing from the response body
