# Reliability battery — second-tier agent

_model `qwen2.5-coder:14b`, 3 run(s)/task, gate = go build+test, resets to runtime HEAD between runs, generated 2026-05-19_

| task class | runs | success | punt | error | mean steps | mean secs | local tokens | frontier tokens |
|---|--:|--:|--:|--:|--:|--:|--:|--:|
| scaffold | 3 | 100% | 0% | 0% | 2.0 | 8 | 15285 | 0 |
| rename | 3 | 100% | 0% | 0% | 3.0 | 9 | 23082 | 0 |
| movefunc | 3 | 100% | 0% | 0% | 2.0 | 6 | 15429 | 0 |
| analysis | 3 | 100% | 0% | 0% | 2.0 | 8 | 15663 | 0 |
| infeasible | 3 | 0% | 100% | 0% | 1.0 | 3 | 7554 | 0 |
| **all** | 15 | 80% | 20% | 0% | - | 7 | 77013 | **0** |

## Reading this

- **success** = task done AND `go build`+`go test` green (gate is ground truth); for `analysis` it is a `report` answer (no gate — nothing changed).
- **punt** = junior cleanly handed back (warm report, repo restored) — a *correct* outcome for `infeasible`, a miss for `scaffold`/`rename`/`movefunc`/`analysis`.
- **error** = infrastructure failure (should be ~0).
- **mean secs** = wall-clock per run. The junior is free in frontier tokens but not in time: if it is too slow the senior won't delegate to it, so this is the real adoption gate, tracked alongside tokens.
- **frontier tokens = 0**: every run is entirely local; each success is frontier spend avoided, each punt costs the senior only a warm report.

