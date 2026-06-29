# Design note: graded sensors + best-of-N, from the ComPilot paper

**Source:** Merouani, et al., *Agentic Auto-Scheduling: An Experimental Study of
LLM-Guided Loop Optimization* (PACT 2025). arXiv:[2511.00592](https://arxiv.org/abs/2511.00592).

This note records what the paper is, why it is architecturally relevant to
gorefactor, and the two concrete changes worth considering. It is a design
memo, not a committed plan — nothing here is implemented yet.

## What the paper does

**ComPilot** puts an off-the-shelf LLM (no fine-tuning, zero-shot) in a closed
loop with a compiler. Each round:

1. the LLM proposes a loop transformation (tiling, fusion, interchange, …) from
   a fixed vocabulary;
2. the compiler **checks legality** and, if legal, **measures real speedup or
   slowdown**;
3. that empirical result is fed back, and the LLM refines its next proposal.

With a *best-of-5* strategy it reaches a 3.54× geomean speedup over the Pluto
polyhedral compiler on PolyBench.

The domain (numeric loop-nest performance) has nothing to do with Go
refactoring, so no code or technique transfers directly. The value is the
*architecture*, which is the same shape as ours.

## Why it maps onto gorefactor

| ComPilot | gorefactor |
|----------|------------|
| LLM proposes transformations from a fixed vocabulary | agent fills a plan schema from the op catalog (`orchestrator`) |
| compiler refuses illegal transformations | guides refuse malformed Go — parse-before-write (`orchestrator`, direct-op commands) |
| compiler measures speedup → feedback | `lint` / `review` / `doctor` sensors → feedback (`agent_tools_sense.go`) |
| off-the-shelf LLM, zero-shot, iterate | `RunDriver` loop, `gorefactor-agent` (`loop.go`) |

This is independent confirmation, from a top compiler venue, of the
guides/sensors harness thesis already in `CLAUDE.md`. It also exposes two gaps.

## Gap 1 — the gate is binary; ComPilot's signal is graded

`runGate` (`cmd/gorefactor-agent/loop.go:187`) is `go build ./...` then
`go test ./...` — pass/fail. The agentic loop treats a green gate as the single
terminal success (`loop.go:145`). That tells the agent *whether* a refactor is
admissible, not *how good* it is. ComPilot's whole engine is the *graded* signal
(measured speedup) the LLM can hill-climb.

We already have the raw material for a graded refactoring signal: a structured
`lint` run. `gorefactor lint <path> --json` enumerates findings with severities
across 25 rules; `senseLint` (`agent_tools_sense.go:93`) already shells out to
it. A score as simple as a severity-weighted finding count, compared
before vs. after a candidate, turns the binary gate into a gradient:

```
score(tree) = Σ over lint findings of weight(severity)
delta       = score(before) − score(after)   # >0 means the refactor improved structure
```

The gate stays the admissibility filter (build+test must pass); the lint delta
becomes the *quality* signal layered on top.

## Gap 2 — single trajectory; ComPilot's headline result is best-of-N

`RunDriver` runs one trajectory: propose → apply → gate, iterating only on
*failure* (`loop.go:59-157`). ComPilot's 3.54× is *best-of-5* — sample several
independent attempts and keep the best by the graded signal. Single-best-effort
leaves the largest, cheapest lever on the table.

Sketch of a best-of-N selection layer over the existing loop, reusing the
clean-worktree + `git reset --hard` rollback that already backstops every apply
(`requireCleanWorktree`, `rollback` in `loop.go`):

```
base := score(".")                      # lint score of clean tree
candidates := []
for n := 1..N {                         # N independent samples (vary temperature/seed)
    plan := propose()                   # existing schema-completion path
    apply(plan)
    if ok, _ := runGate("."); ok {      # admissibility filter, unchanged
        candidates = append(candidates, {plan, delta: base - score(".")})
    }
    rollback(cfg.Dir, cfg.Out)          # always reset between samples
}
winner := argmax(candidates, .delta)    # may be empty → fall back to current behaviour
reapply(winner.plan)                    # leave the best one in the tree
```

This composes with Gap 1: best-of-N needs a ranking key, and the lint delta is
it. Neither change alters the safety model — the build/test gate and git
rollback are untouched; we only add a scorer and an outer sampling loop.

## What this is *not*

- Not a reason to fine-tune. ComPilot's point is that off-the-shelf models
  suffice given a good feedback loop — same bet we already make.
- Not portable code. The polyhedral/legality machinery is compiler-specific.
- Not free: best-of-N multiplies model + gate cost by N. The paper's N=5 is a
  reasonable starting point to measure cost/quality against single-shot.

## Suggested first step

Add `gorefactor lint --json`-backed scoring as a standalone, testable function
(no loop changes yet), then wire it as a *reported* metric in the agentic loop
to observe deltas on real runs before adding the best-of-N selection layer.
