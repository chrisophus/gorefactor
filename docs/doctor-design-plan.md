# gorefactor doctor: Design Plan

Status: purified via AISP 5.1 (source: `gorefactor-doctor-v2.aisp`)
Date: 2026-07-16

## Summary

Doctor becomes a standalone Go codebase health tool, playing the role for Go that react-doctor plays for React: deterministic detection of the code quality problems coding agents introduce, cheap enough to run on every edit, with diagnostics structured for both humans and agent loops. The gorefactor agent loop becomes one consumer of doctor rather than its container. The `finish` gate is doctor.

Resolved decisions: 1a, 2a (refined), 3b, 4b, 5a, 6a. See Decisions section.

## Goals

1. Any Go repo can run doctor with one command, no gorefactor loop required.
2. The gorefactor agent loop uses doctor as its finish gate, closing the seam where campaigns could succeed on build+test alone.
3. Failed gates escalate to the expensive model using findings as the prompt, never full files.
4. A prevention loop pushes doctor's rules into agent context so findings trend toward zero at generation time.

## Non-Goals

- A bespoke rule engine. All detection reuses the go/analysis ecosystem.
- A calibrated 0-100 score. Deferred to a presentation layer; nothing gates on it.
- Fixing findings. Doctor detects; the agent loop (or a human) fixes.

## Architecture

### Detection substrate

Doctor orchestrates existing analyzers and merges their output into one report:

- **golangci-lint** (JSON output): the breadth layer. staticcheck, gocritic, gosec, unused, and the standard set.
- **govulncheck**: call-graph-aware vulnerability detection. Only reachable vulns report.
- **deadcode** (golang.org/x/tools): whole-program unused-code detection.
- **apidiff** (built on go/packages + go/types): exported-API surface diff against a base ref. This is the semantic-preservation check that build+test cannot provide and has no react-doctor analog.
- **Custom go/analysis analyzers**: shape-conditioned rules, starting with Temporal workflow constraints.

### Shape detection

Doctor sniffs project shape before selecting rules, the analog of react-doctor detecting Next vs Vite vs React Native:

- Service vs library (cmd/ entrypoints, main packages)
- Temporal client presence: enables workflow-determinism rules (no time.Now, no naked goroutines, no math/rand inside workflow code)
- Kafka client presence: reserved for future consumer-hygiene rules
- Go version and generics usage

Shape conditions severity too: os.Exit is a finding in a library, not in main.

### Categories and severity

| Category | Contents | Severity |
|---|---|---|
| conc | goroutine leaks, unguarded shared state, context misuse | error |
| sec | gosec findings, new reachable vulns | error |
| api | undeclared exported-API changes | error |
| tmprl | workflow-determinism violations | error |
| perf | large copies, allocation patterns, string concat in loops | warning |
| dead | unused code, exports, dependencies | warning |

Severity is derived from category. Error-severity findings gate; warnings report.

### Diff-first execution

One rule set. No fast/full tier split. Speed comes from scoping and caching, following react-doctor's resolution of the same problem:

- **Scope**: each run covers the packages touched by the edit plus their direct reverse dependencies.
- **Cache**: content-hashed, built to survive fresh checkouts (react-doctor's hard-won lesson: stat-keyed caches never hit on re-cloned trees).
- **Baseline marking**: every finding is marked new or pre-existing against a base ref (main by default). Pre-existing findings never block; they appear in reports.

### The gate

`finish` in the agent loop is:

```
build && test && (no new error-severity findings in scoped doctor run)
```

Campaign completion additionally requires a full-repo doctor pass with no new error-severity findings against the campaign's starting ref.

**Rollout is advisory-first** (react-doctor's trust-before-gate default): doctor reports without blocking until its false-positive profile on MCT has been observed. Rules that prove flaky or noisy are excluded or demoted in config, not tolerated.

### API intent declarations

The refactor plan schema gains an `intent` field. An apidiff delta with a matching `intent: api-change` declaration passes; an undeclared delta fails the gate, citing the diff. The gate checks declared intent against observed change. This keeps unattended campaign mode viable: no human pause, but no silent API drift either.

### Escalation contract

When the gate fails, the escalation prompt to the expensive model is the findings themselves: file, line, rule ID, category, plus minimal surrounding context. The full file is never sent. This is the token-economy payoff: deterministic detection produces exactly the context the expensive model needs.

### Prevention loop (agent-install mode)

`gorefactor doctor install <agent>` emits doctor's rule expectations into agent context (CLAUDE.md, skills directory), mirroring react-doctor's agent-install mode. The generating model sees the rules before writing code. Success metric: new findings per edit trending down over time.

### Programmatic API

A `diagnose(repo, baseRef) -> Report` Go API serves the agent loop, CI, and any other consumer identically. The CLI is a thin wrapper over it.

## Report structure

```go
type Report struct {
    Findings []Finding // file, line, rule, category, severity, new(bool)
    NewCount, FixedCount int // per severity
    Score *float64 // optional, presentation only, initially nil
}
```

## Error handling

| Condition | Response |
|---|---|
| Gate run exceeds acceptable latency | Narrow scope (drop reverse deps) or warm cache; measure before adding tiers |
| Rule produces nondeterministic results | Exclude and log |
| Rule false-positive rate climbs | Demote severity in config |
| Vuln exists on main | Baseline: reported, never blocks |
| apidiff delta without declared intent | Fail gate, cite the diff |

## Decisions log

1. **(1a)** Doctor absorbs the finish gate. The finish/doctor split was the seam that let campaigns succeed on the weak bar.
2. **(2a, refined)** One rule set, package-scoped runs, persistent cache. The fast/full rule split from earlier drafts is dropped; react-doctor demonstrated diff scoping plus caching solves latency without rule tiers.
3. **(3b)** Category-derived severity; errors gate, warnings report. Blocking on dead code trains gate bypass.
4. **(4b)** No score initially. Counts and deltas provide the same gating power with nothing to calibrate. Score is a future presentation layer.
5. **(5a)** Plan-level intent declarations resolve deliberate API changes. Human confirmation would break unattended campaigns.
6. **(6a)** New findings only gate. Direct react-doctor precedent: diff mode fails on new regressions without dragging in baseline backlog.
7. Advisory-first rollout before the gate goes hard (borrowed from react-doctor's blocking:none default).

## Open items (empirical, not design)

1. **The load-bearing measurement**: warm-cache, package-scoped golangci-lint latency on MCT-scale code. The single-rule-set bet rests on this number being small (seconds). Measure before building the cache layer.
2. Temporal analyzer rule inventory: enumerate the workflow-determinism violations worth catching first, from MCT's actual Temporal usage.
3. Reverse-dependency depth for scoping: depth-1 is the starting assumption; validate that depth-1 catches cross-package breakage apidiff misses.

## Build order

1. `diagnose` API wrapping golangci-lint JSON + baseline marking against a ref (advisory value from day one)
2. apidiff integration + plan `intent` field
3. Gate wiring: finish = build + test + no new errors; campaign full-pass
4. govulncheck + deadcode + shape detection
5. Temporal custom analyzers
6. Agent-install mode
7. Score layer (optional, last)
