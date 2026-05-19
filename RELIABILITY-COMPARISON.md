# Junior-model A/B: local Ollama vs Anthropic Haiku

_Controlled comparisons against the gorefactor-agent harness. Same
binary, same target commit per battery, repo reset between every run.
Only the second-tier ("junior") model varies. Generated 2026-05-19,
branch `compare-junior-models`._

This document records three loops:

1. **Base battery** (`scripts/reliability.sh`, 5 task classes).
2. **Triage savings** (Cases 1+2: positive shortcut for single-CLI-op
   specs; negative warm-punt for explicit-judgement specs).
3. **Hard battery** (`scripts/reliability-hard.sh`, 3 classes designed
   to discriminate where the base set saturates) — to answer "where, if
   anywhere, is the paid API junior better?".

---

## 1. Base battery — same correctness profile; qwen ~8× cheaper

| task class | qwen2.5-coder:14b (Ollama, local/free) | claude-haiku-4-5 (Anthropic, paid) |
|---|---|---|
| scaffold   | ✅ 100% · 2.0 steps · 18s · 10,302 tok | ✅ 100% · 3.0 steps · 22s · 23,520 tok |
| rename     | ✅ 100% · 3.0 steps · 10s · 15,556 tok | ✅ 100% · 9.0 steps · 24s · 92,912 tok |
| movefunc   | ✅ 100% · 2.0 steps · 6s · 10,398 tok  | ✅ 100% · 11.0 steps · 18s · 105,902 tok |
| analysis   | ✅ 100% · 2.0 steps · 8s · 10,554 tok  | ✅ 100% · 3.0 steps · 7s · 28,172 tok |
| infeasible | ✅ punt · 1.0 step · 2s · 5,092 tok    | ✅ punt · 12.0 steps · 26s · 178,079 tok |
| **all (10 runs)** | **80% success / 20% correct-punt / 0% error** · ~9s · 51,902 tok | **80% success / 20% correct-punt / 0% error** · ~20s · 428,585 tok |

Identical correctness; Haiku ~8.3× tokens, ~2.2× slower, far more
steps. The most telling difference is `infeasible`: qwen punts in 1
step / 2s / 5K tok — the ideal warm handback — while Haiku grinds 12
steps / ~89K tok before punting on tool-call budget exhaustion. Same
verdict, ~18× cost to reach it.

## 2. Triage guides cut the bill ~70% on Haiku

The base table shows two patterns:

- `rename`, `analysis` each map 1:1 to one gorefactor CLI command, so
  any LLM call is wasted (~10–100K tokens for what the CLI does for 0).
- `infeasible` is a 100% punt by design; Haiku's expensive flail to
  reach it is also waste — the verdict was knowable from the spec.

Two deterministic guides in `cmd/gorefactor-agent/triage.go` close
both misroutes. They run before any provider is allocated.

**Positive triage (Case 1, commit `14bbfd3`).** Regex-matched patterns
that map 1:1 to one op apply it, run the build/test gate, emit
`RUN_METRICS`, and exit — no LLM call. Patterns v1: `rename` →
`rename_declaration`, `callers of X` / `who calls X` →
`find_references`.

**Negative triage (Case 2, commit `8a46f82`).** Specs that *explicitly*
require semantic judgement (`rewrite ... for performance / linear-time
/ memory`, `optimize ... allocation`, `redesign`, `fix race`,
`fix deadlock`, `fix leak`, ...) emit `doPunt` directly — same warm
hand-off the LLM would produce, at 0 tokens.

**Safety guard (commit `9f2fade`).** Specs carrying an explicit negative
constraint ("do not", "but not", "leave ... untouched", "only on", ...)
fall through to the agent even if a positive pattern matches. Surfaced
by the hard-battery disambig run (§3 below).

### Token savings — same battery, after-triage

| task class | qwen before | qwen after | Haiku before | Haiku after |
|---|--:|--:|--:|--:|
| scaffold | 10,302 | 10,338 | 23,520 | 23,590 |
| rename | 15,556 | **0** | 92,912 | **0** |
| movefunc | 10,398 | 10,434 | 105,902 | 106,164 |
| analysis | 10,554 | **0** | 28,172 | **0** |
| infeasible | 5,092 | **0 (autopunt)** | 178,079 | **0 (autopunt)** |
| **total** | 51,902 | **20,772 (–60%)** | 428,585 | **129,754 (–70%)** |

All correctness retained at 80% success / 20% correct-punt for both
juniors. Wall-time also drops in proportion.

## 3. Hard battery — where, if anywhere, does Haiku win?

The base battery saturates both juniors at 80/20, so it cannot
discriminate where one is genuinely more capable than the other.
`scripts/reliability-hard.sh` adds three task classes designed to
stress different modes:

- **multistep** — Move F to a new file *then* rename F + update
  callers. Tests sequencing of dependent ops in one spec.
- **disambig** — Rename a method on one receiver only; same-named
  method on another receiver must stay untouched. Tests receiver-
  scoped symbol disambiguation.
- **multifile** — Add a leading parameter to an unexported function
  and update every caller (~7 sites, 4 files). Tests cross-file
  mechanical change with no single gorefactor op for "add parameter".

| task class | qwen2.5-coder:14b | claude-haiku-4-5 |
|---|---|---|
| multistep | ✅ 100% · 4.0 steps · 17s · 10,867 tok | ❌ **100% punt** · 12.0 steps · 27s · 63,157 tok |
| disambig  | ✅ 100% (triage)\* · 0 tok | ✅ 100% (triage)\* · 0 tok |
| multifile | ❌ 100% punt · 6.0 steps · 36s · 17,199 tok | ❌ 100% punt · 12.0 steps · 20s · 77,077 tok |

\* The disambig result was contaminated: the original "Rename Tokens to
TokenUsage. Do NOT rename ..." matched the positive-triage rename regex,
and the build+test gate stayed green even when both `Tokens` methods
were renamed in lock-step (the package stays internally consistent).
That is exactly the failure mode the safety guard in commit `9f2fade`
closes — future runs of this spec will fall through to the agent.

### Verdict

**In this task space — gorefactor-driven mechanical refactors gated by
go build+test — there is no task class where Haiku outperforms the local
qwen junior.**

- **multistep**: qwen succeeds at 11K tokens. Haiku PUNTS at 63K tokens.
  This is a regression for Haiku, not a win.
- **disambig**: contaminated by triage; no signal either way.
- **multifile**: both punt; Haiku burns 4.5× more tokens to reach the
  same punt.

Across the base battery (5 classes) AND the hard battery (3 classes)
the local qwen junior is **equal-or-better** on every task. The
hypothesis "Haiku is harder-task-better" is *falsified* for this
codebase's gorefactor task space.

### Caveats

- **Sample size**: ITERS=2; small. qwen is deterministic for a fixed
  binary+commit so its numbers are stable; Haiku at temperature=0 was
  near-deterministic across both iters except where the flailing path
  itself was noisy.
- **Task design**: the hard set probes mechanical stress modes
  (sequencing, disambiguation, cross-file). It does not probe
  open-ended natural-language reasoning, very long context, or
  algorithmic synthesis. A different task space might still favor
  Haiku; the gorefactor task space does not.
- **Haiku's punts were tool-call budget exhaustion** at `-max-iter 12`,
  not "this is impossible". With a higher cap Haiku might finish
  multistep. But for the two-tier harness, where the junior's value
  is "fail fast and warm-punt", uncapped iteration is the wrong
  design point — and qwen finishes the same task in 4 steps anyway.

## 4. Routing policy implied by this data

Strictly preferred for this codebase, in order:

1. **Tier 0 — direct gorefactor CLI** (`gorefactor rename`, `move`,
   `find-callers`, `split`, `lint --fix`, ...): 0 tokens.
2. **Tier 1 — triage guide** (`cmd/gorefactor-agent/triage.go`):
   single-CLI-op specs / explicit-judgement specs — 0 tokens; same
   correctness as Tier 0 / a perfect warm punt.
3. **Tier 2 — local junior (qwen)**: multi-step mechanical work that
   needs the gate but no judgement. ~5–17K tokens / run, local-free.
4. **Tier 3 — frontier (senior, you/me)**: anything the local junior
   punts. Pays for itself by handling judgement the junior shouldn't
   attempt.

There is no Tier 2.5 (paid API junior) — Haiku is not above the local
junior on these tasks and not below the senior, so it has no place
in the routing.

## 5. Harness defects surfaced and fixed in the course of this work

This A/B was only obtainable after fixing four real bugs the
local-only battery never exercised. All are committed on
`compare-junior-models`.

1. `provider_anthropic.go` had **no retry/backoff** on 429/5xx
   (commit `d6d8156`).
2. `reliability.sh` `${3:-…}` clobbered an explicit empty api-base,
   pointing the anthropic provider at the Ollama URL (`d228502`).
3. The original triage exited 1 on autopunt instead of 3
   (`0f00634`).
4. Positive triage matched `rename X to Y` even when the spec carried
   a "do not rename Z" constraint the gate couldn't catch
   (`9f2fade`).
