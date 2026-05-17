# Reliability battery — second-tier agent

_model `qwen2.5-coder:14b`, 3 run(s)/task, gate = go build+test, resets to runtime HEAD between runs, generated 2026-05-17_

**Headline: 9/9 intended outcomes, 0 errors, 0 frontier tokens.** scaffold and
rename succeed every run (gate-green); the deliberately-infeasible task is
*correctly* punted every run (the right outcome — punt is success for that
class). Token use is tiny and deterministic (~2.6k local tokens/success,
~1.3k/punt) — the headline metric is that **every one of these tasks was
cleared or cleanly escalated by the cheap local model with zero frontier
spend.**

| task class | runs | success | punt | error | mean steps | local tokens | frontier tokens |
|---|--:|--:|--:|--:|--:|--:|--:|
| scaffold | 3 | 100% | 0% | 0% | 2.0 | 7929 | 0 |
| rename | 3 | 100% | 0% | 0% | 2.0 | 7893 | 0 |
| infeasible | 3 | 0% | 100% | 0% | 1.0 | 3915 | 0 |
| **all** | 9 | 67% | 33% | 0% | - | 19737 | **0** |

## Reading this

- **success** = task done AND `go build`+`go test` green (gate is ground truth).
- **punt** = junior cleanly handed back (warm report, repo restored) — a *correct* outcome for `infeasible`, a miss for `scaffold`/`rename`.
- **error** = infrastructure failure (should be ~0).
- **frontier tokens = 0**: every run is entirely local; each success is frontier spend avoided, each punt costs the senior only a warm report.

