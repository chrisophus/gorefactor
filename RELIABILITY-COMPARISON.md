# Junior-model A/B: local Ollama vs Anthropic Haiku

_Controlled comparison, generated 2026-05-19. Same `gorefactor-agent`
binary, same target commit (`d228502`), same battery (`scripts/reliability.sh`),
ITERS=2 per task class, gate = `go build`+`go test`, repo reset between
every run. Only the second-tier ("junior") model varies._

## Result

| task class | qwen2.5-coder:14b (Ollama, local/free) | claude-haiku-4-5 (Anthropic, paid) |
|---|---|---|
| scaffold   | ✅ 100% · 2.0 steps · 18s · 10,302 tok | ✅ 100% · 3.0 steps · 22s · 23,520 tok |
| rename     | ✅ 100% · 3.0 steps · 10s · 15,556 tok | ✅ 100% · 9.0 steps · 24s · 92,912 tok |
| movefunc   | ✅ 100% · 2.0 steps · 6s · 10,398 tok  | ✅ 100% · 11.0 steps · 18s · 105,902 tok |
| analysis   | ✅ 100% · 2.0 steps · 8s · 10,554 tok  | ✅ 100% · 3.0 steps · 7s · 28,172 tok |
| infeasible | ✅ punt · 1.0 step · 2s · 5,092 tok    | ✅ punt · 12.0 steps · 26s · 178,079 tok |
| **all (10 runs)** | **80% success / 20% correct-punt / 0% error** · ~9s · 51,902 tok | **80% success / 20% correct-punt / 0% error** · ~20s · 428,585 tok |

## Reading it

- **Correctness is identical.** Both models pass scaffold/rename/movefunc/analysis
  and correctly punt the `infeasible` task (rolling-hash rewrite needs semantic
  judgement). On this mechanical task set Haiku is **not more capable** than the
  free local junior.
- **Cost differs sharply.** Haiku used **~8.3× more tokens** (428.6K vs 51.9K),
  ran **~2.2× slower** (20s vs 9s/run), and took far more tool-call steps
  (movefunc 11 vs 2; rename 9 vs 3). qwen's tokens are local/free; Haiku's are
  paid API.
- **Punt quality differs — the important one for a two-tier harness.** qwen
  punts `infeasible` in **1 step / 2s / 5K tokens** — an immediate, cheap warm
  handback, exactly what the junior tier is for. Haiku grinds **12 steps / 26s /
  ~89K tokens** before punting on *tool-call budget exhaustion* rather than
  recognising infeasibility up front. Same outcome, far more expensive path.

## Conclusion

For the local junior tier on this battery, **the Ollama model is the better
choice**: same pass/punt profile, an order of magnitude cheaper, faster, and it
fails fast instead of flailing. An API junior like Haiku earns its cost only on
tasks harder than this set (where the local model would miss), not here.

## Determinism / confidence

- qwen is deterministic for a fixed binary+commit (token counts identical across
  iters); the ITERS=2 numbers match the committed ITERS=3 `RELIABILITY.md`
  (80/20), so the small N is reliable.
- Haiku ran at `temperature=0`: scaffold/rename/movefunc/analysis token counts
  were identical across both iters; only `infeasible` varied (the flailing path
  is the noisy one).

## Harness defects surfaced and fixed in the course of this comparison

This A/B was only obtainable after fixing two real bugs the local-only battery
never exercised:

1. **`provider_anthropic.go` had no retry/backoff** (commit `d6d8156`). A single
   HTTP 429/5xx aborted the whole agent run as `error`. Local Ollama has no rate
   limit, so this gap was invisible until an API model was dogfooded as junior.
   Now: shared `doWithRetry`, 5 attempts, exponential backoff, retries 429 / 5xx
   / transport errors.
2. **`reliability.sh` `${3:-…}` clobbered an explicit empty api-base** (commit
   `d228502`). Passing `""` to mean "use the provider's own default" was
   substituted with the Ollama URL, so the Anthropic provider POSTed to
   `localhost:11434/v1/v1/messages` → instant 404 on every run. Fixed with
   `${3-…}` (substitute only when *unset*). The provider-aware battery change is
   `c71484f`.
