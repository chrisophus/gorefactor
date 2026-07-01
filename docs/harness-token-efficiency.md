# Harness & Token-Efficiency Work — Implementation Status

Tracks the "Research-Driven Harness and Token Efficiency" plan (2026-07-01).
Each phase names the specific cost it removes and confirms that cost is
present before optimizing (the plan's design rule).

## What shipped (code, in this repo)

### Phase 1 — Tool-output masking (`cmd/gorefactor-agent/history_mask.go`)

Cost removed: input-token accumulation from stale tool outputs re-sent every
round. Confirmed present: the agentic loop carried full tool history forward.

`maskStaleToolOutputs(msgs, maskAfterRounds)` replaces the body of every
tool result older than the last `maskAfterRounds` (=3) assistant turns with a
one-line structured stub (`[elided: lint_path result, 1423 bytes; …]`).
Recency is the **sole** trigger. Structure is preserved (message count and
ordering unchanged) so the provider invariant "a tool message follows its
tool_call" holds. Applied at prompt-assembly time only — the raw transcript
and the Phase 6 corpus are untouched. Wired into both `RunAgenticDriver` and
`RunInteractiveAgenticDriver`, composed with the existing `compactMessages`
window.

Never masked (by construction of the recency cutoff): the system prompt, the
task objective (a user message), and the most-recent error (always inside the
keep window). `N=3` is the starting value; tune against the Phase 0 baseline.

### Phase 2 — Runtime token budget with stop-and-summarize

Cost removed: spend past the accuracy plateau. Confirmed present: usage was
tracked but nothing enforced a ceiling.

`-budget N` flag (0 = unlimited) on `gorefactor-agent`. Before each model
round the loop checks cumulative prompt+completion tokens; on exhaustion it
emits a **structured punt** (`autopunt:budget_exhausted`) rather than killing
mid-edit — the journal/undo system makes this safe. `RunCampaign` enforces the
same value as an aggregate cap across findings and stops cleanly instead of
churning every remaining finding into a punt. Budget hits are logged to the
Phase 6 corpus.

Default is unlimited: the plan's computed default (smallest budget `b` where
`success_rate(b) ≥ 0.95·success_rate(unbudgeted)`) requires the Phase 0
dataset and is **deferred** to that measurement.

### Phase 3 — Blast-radius instrumentation (`cmd/gorefactor-agent/instrument.go`)

Cost removed: misrouted tasks. Confirmed present: routing is judgment-based.

`specBlastRadius` extracts the primary target symbol from the spec and runs
`gorefactor blast-radius --json`, emitting the composite score in every
`RUN_METRICS` block next to actual tokens spent and outcome. This is **pure
instrumentation** — routing is *not* wired, because the plan gates that on a
measured correlation beating the 0.39 self-prediction bar, which needs the
Phase 0 dataset. The data needed to compute that correlation is now logged.

### Phase 4 — Persistent cross-session notes (`cmd/gorefactor-agent/notes.go`)

Cost removed: re-discovery tokens at session start. Confirmed present: warm
punt state died with the process.

`.gorefactor/notes.md` is loaded into the system prompt at agent start and
appended only via the dedicated `note` tool (categories: `repo_fact`,
`failed_strategy`, `flaky_test`, `open_punt`) — never a free-form file write,
preserving the harness principle. Punts also auto-record an `open_punt` note so
the next session does not re-attempt known-infeasible work. When notes.md
crosses `notesCompactionThreshold` (=200 lines) `appendNote` emits an advisory
that a crucible purify pass is due.

The crucible purify compaction itself is **deferred**: it requires the crucible
`purify` binary, which is not present in this repo.

### Phase 6 — Failure corpus (`cmd/gorefactor-agent/corpus.go`)

Cost removed: repeated mistakes. Confirmed present: rejections happened and
nothing accumulated them.

Every rejected mutation op (`recordRejectedOp`), every budget hit, and every
punt is appended to `.gorefactor/failures.jsonl` — a passive sensor that never
gates a run. `.gorefactor/` is gitignored, so the corpus survives the agent's
`git clean -fd` rollback across attempts (its whole point). The manual
classification pass (every 25 entries → lint rule / prompt amendment / new
capability) and the crucible-purify check on prompt amendments are process
steps, not code.

## Deferred (require external infrastructure, not code in this repo)

- **Phase 0 — benchmark re-baseline.** Needs live frontier + local model runs
  (≥5 per scenario) with API keys. Every measurement-based exit criterion below
  depends on it.
- **Phase 5 — crucible purification at the two spec chokepoints.** Needs the
  crucible `purify` pipeline binary. Integration points identified: campaign
  objective (`campaign.go`, before loop entry) and senior→junior handoff
  (`campaign_run.go`). 5b is explicitly droppable on its own break-even
  inequality once Phase 0/2 data exists.
- **All A/B measurements and correlation computations** in Phases 1–5. The code
  emits the signals (masked-history token counts, `blast_radius` in RUN_METRICS,
  budget events); computing the deltas is the deferred experiment.

## Exit criteria still open (measurement)

1. Cumulative frontier prompt tokens per scenario ↓ ≥50% vs Phase 0 (Phase 1).
2. Cross-run token variance compressed toward single digits (Phases 2, 5a).
3. A routing signal with correlation > 0.39 to spend, or a documented negative
   result (Phase 3).
4. First-three-rounds token cost lower on repos with mature notes (Phase 4).
5. Zero recurrence of classified failure patterns post-fix (Phase 6).
