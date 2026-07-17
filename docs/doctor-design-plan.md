# gorefactor doctor: Design Plan

Status: purified via AISP 5.1 (source: `gorefactor-doctor-v2.aisp`); revised 2026-07-17 after codebase-integration review. **Build order steps 1–3 are implemented** (`doctor/` package, `gorefactor doctor --report`, `gorefactor intent`, agent `-doctor-gate`); see per-step status in Build order.
Date: 2026-07-16

## Summary

Doctor becomes a standalone Go codebase health tool, playing the role for Go that react-doctor plays for React: deterministic detection of the code quality problems coding agents introduce, cheap enough to run on every edit, with diagnostics structured for both humans and agent loops. The gorefactor agent loop becomes one consumer of doctor rather than its container. The `finish` gate is doctor.

This is an evolution of the existing `doctor` command, not a greenfield build: the structural linter, `api-diff`, the baseline fingerprint machinery, and the verified autofix pass all carry forward (see Integration with existing machinery).

Resolved decisions: 1a, 2a (refined), 3b, 4b, 5a (refined), 6a, plus 8–14 from the integration review. See Decisions section.

## Goals

1. Any Go repo can run doctor with one command, no gorefactor loop required.
2. The gorefactor agent loop uses doctor as its finish gate, closing the seam where campaigns could succeed on build+test alone.
3. Failed gates escalate to the expensive model using findings as the prompt, never full files.
4. A prevention loop pushes doctor's rules into agent context so findings trend toward zero at generation time.

## Non-Goals

- A *new* bespoke rule engine. Detection reuses the go/analysis ecosystem plus gorefactor's existing in-process structural linter; no third engine gets built.
- A calibrated 0-100 score. Deferred to a presentation layer; nothing gates on it.
- Fixing findings inside `diagnose`. Detection and fixing stay separate; findings may carry autofix *hints* (`FixCmd`), and `doctor --fix` remains the verified apply path (see Autofix disposition).

## Integration with existing machinery

The plan lands in a codebase that already has half of it. Disposition of each existing piece:

| Existing piece | Disposition |
|---|---|
| Structural lint (28 in-process rules, `cmd_lint_*.go`) | **Kept as a first-class detection substrate** alongside golangci-lint. It catches what golangci does not (duplicate-block, blast-radius, funcorder autofix pairing, adherence) and is the sensor half of the repo's guide→sensor→autofix principle. Not ported to go/analysis; wrapped by `diagnose`. |
| `api-diff` (`analyzer.APIDiffResult`) | **Reused and extended.** The apidiff substrate builds on the existing exported-surface differ; work is intent matching and gate wiring, not a new differ. |
| Lint baseline (`cmd_lint_baseline.go` fingerprints) | **Reused as the baseline-marking mechanism** for all substrates (see Diff-first execution). The snapshot-file ratchet (`--baseline`/`--write-baseline`) remains for its existing adoption use case. |
| Doctor stage runner (`cmd_doctor.go`) | **Evolves into the merge layer**: stages become substrates whose findings merge into one Report instead of pass/fail lines. |
| Agent `runGate` (build+test, `loop.go`) | **Replaced** by the doctor gate (build+test remain as two of its checks). |
| `doctor --fix` (verified autofix) | **Kept.** See Autofix disposition. |
| `.gorefactor/` journal + failures corpus | **Extended** with a doctor findings journal (see Advisory-first rollout). |

## Architecture

### Detection substrates

Doctor orchestrates existing analyzers and merges their output into one report:

- **Structural lint** (in-process): gorefactor's own rule set — duplication, size/structure, design smells, error handling, ordering, adherence. Already scope-capable and fast.
- **golangci-lint** (JSON output): the breadth layer. staticcheck, gocritic, gosec, unused, and the standard set.
- **govulncheck**: call-graph-aware vulnerability detection. Only reachable vulns report. Requires network access to the vuln DB.
- **deadcode** (golang.org/x/tools): whole-program unused-code detection.
- **apidiff** (extending the existing `api-diff`): exported-API surface diff against a base ref. This is the semantic-preservation check that build+test cannot provide and has no react-doctor analog.
- **Custom go/analysis analyzers**: shape-conditioned rules, starting with Temporal workflow constraints.

The merge layer owns cross-cutting filtering: generated files and `.gorefactor.yaml` `walk:` skip rules are applied uniformly to every substrate's findings, because deadcode and apidiff have no native notion of skip configs and golangci's is separate. (Doctor has already had exactly this bug class — `--fix` ignoring `walk:` skip rules — so the exclusion is a merge-layer responsibility, stated here as a contract.)

### Substrate availability

A substrate that cannot run is not the same as a substrate that ran clean, and the two modes treat it differently:

- **Advisory / human runs**: soft-skip with a visible warning, exactly as doctor's golangci stage does today (a version-mismatched or missing binary must not block local commits; CI is the enforcement backstop).
- **Gate runs**: substrate availability is recorded in the Report (`Substrates`). A campaign **fails fast at start** if a gating substrate cannot run — discovering a dark sensor at `finish`, after a full campaign of work, is the worst place to learn it. govulncheck degrades explicitly when the vuln DB is unreachable (offline sandboxes): recorded as unavailable, never silently passed.

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
| struct | structural-lint findings (size, duplication, smells, ordering) | warning |

Severity is derived from category. Error-severity findings gate; warnings report.

### Diff-first execution

One rule set. No fast/full *rule* split. Speed comes from scoping and caching, following react-doctor's resolution of the same problem:

- **Scope**: each run covers the packages touched by the edit plus their direct reverse dependencies.
- **Cache**: content-hashed, built to survive fresh checkouts (react-doctor's hard-won lesson: stat-keyed caches never hit on re-cloned trees).
- **Baseline marking**: every finding is marked new or pre-existing against a base ref (main by default). Pre-existing findings never block; they appear in reports.

**Baseline marking reuses the lint fingerprint machinery** (file + rule + digit-normalized message, line-number-independent) rather than analyzing two trees per run. The base ref's fingerprint set is computed once per base commit and cached keyed by its SHA; per-edit runs analyze only the working tree and subtract. Analyzing the base ref on every gate run would double the latency that open item 1 bets on being small — the hot path must stay single-tree.

**Execution tiers** (not rule tiers — the rules are identical; this is an honest statement of where each substrate *can* run):

| Tier | Substrates | When |
|---|---|---|
| Scope-capable | structural lint, golangci-lint, custom analyzers | every gate run, package-scoped |
| Full-run-only | deadcode (whole-program), govulncheck (call-graph), apidiff (module surface) | campaign completion, CI, explicit full runs |

### The gate

`finish` in the agent loop is:

```
build && test && (no new error-severity findings in scoped doctor run)
```

Campaign completion additionally requires a full-repo doctor pass — which is where the full-run-only substrates execute — with no new error-severity findings against the campaign's starting ref.

**Rollout is advisory-first** (react-doctor's trust-before-gate default): doctor reports without blocking until each rule's false-positive profile has been observed on the reference codebase. Rules that prove flaky or noisy are excluded or demoted in config, not tolerated.

**Graduation criteria**: a rule flips from advisory to gating when the findings journal (below) shows N runs (initially 50) with no confirmed false positives, or by explicit config promotion. Demotion is the same mechanism in reverse and is always available in config — trust is adjusted with data, not debate.

### API intent declarations

Deliberate API changes are declared as a **session-scoped intent record** in `.gorefactor/` (journaled like mutations are), written by any of the mutation surfaces:

- `gorefactor intent api-change <package-or-symbol scope> "reason"` (direct CLI, human or agent tool call)
- an `intent` field in the orchestrator plan schema (plans are one writer of the record, not the only one)
- the agent spec, for campaigns whose purpose is an API change

An apidiff delta **within a declared scope** passes; an undeclared delta fails the gate, citing the diff. Intent is scoped to packages or symbols precisely so a campaign cannot blanket-declare its way past the check — the gate's job is matching the declared scope against the observed delta, not just checking a boolean. This keeps unattended campaign mode viable: no human pause, but no silent API drift either. Intent records expire with the session/campaign that wrote them.

The rationale for records over plan-only fields: the agentic loop mostly issues direct CLI ops (tool calls), not orchestrator JSON plans, and human CLI users have no plan at all — an intent surface only reachable from plans would miss most mutations.

### Escalation contract

When the gate fails, the escalation prompt to the expensive model is the findings themselves: file, line, rule ID, category, message, plus the minimal surrounding context carried in the finding's `Context` field. The full file is never sent. Findings with a `FixCmd` hint escalate as "run this command" rather than as reasoning work — cheaper still. This is the token-economy payoff: deterministic detection produces exactly the context the expensive model needs, and mechanical fixes bypass the model entirely.

### Autofix disposition

`diagnose` never mutates — detection and fixing stay separate. But the existing verified autofix pass (`doctor --fix`: every fix build+test gated, reverted individually on failure) is the repo's documented trust unlock for unsupervised cleanup and is kept as-is. The bridge between the two is the `FixCmd` hint on findings: where a finding maps to an existing autofix (`remove-log-return`, `wrap-errors`, `wrap-sentinels`, `split`, `reorder-funcorder`, …), the finding says so, and both the agent loop and `doctor --fix` consume the same mapping. This preserves the guide→sensor→autofix principle: every new doctor rule should state its `FixCmd` or explicitly declare the fix judgment-required.

### Prevention loop (agent-install mode)

`gorefactor doctor install <agent>` emits doctor's rule expectations into agent context (CLAUDE.md, skills directory), mirroring react-doctor's agent-install mode. The generating model sees the rules before writing code. Success metric: new findings per edit trending down over time, measured from the findings journal.

### Programmatic API

A `diagnose(repo, baseRef) -> Report` Go API serves the agent loop, CI, and any other consumer identically. The CLI is a thin wrapper over it.

## Report structure

```go
type Report struct {
    SchemaVersion int
    Findings   []Finding
    Substrates []SubstrateStatus   // per substrate: ran | skipped(reason) | failed(reason)
    NewCount   map[Severity]int    // new findings per severity
    FixedCount map[Severity]int    // baseline findings absent from this run, per severity
    Score      *float64            // optional, presentation only, initially nil
}

type Finding struct {
    File, Message string
    Line          int
    Rule          string   // rule ID within its substrate
    Substrate     string   // provenance: structural | golangci | govulncheck | deadcode | apidiff | custom
    Category      Category // conc | sec | api | tmprl | perf | dead | struct
    Severity      Severity // derived from category, demotable in config
    New           bool     // vs base-ref fingerprint set
    Fingerprint   string   // stable ID (lint fingerprint scheme); baseline matching + suppression config key
    FixCmd        string   // optional: gorefactor command that mechanically fixes this finding
    Context       string   // minimal surrounding source for escalation prompts
}
```

The struct is the shared contract for every consumer (agent loop, CI, escalation prompt assembly), so it is specified here rather than left to implementation. `SchemaVersion` guards the JSON output.

## Error handling

| Condition | Response |
|---|---|
| Gate run exceeds acceptable latency | Narrow scope (drop reverse deps) or warm cache; measure before adding tiers |
| Substrate binary missing / can't run (advisory mode) | Soft-skip with visible warning (current doctor golangci behavior) |
| Substrate can't run (gate mode) | Recorded in `Report.Substrates`; campaign fails fast at start, not at finish |
| govulncheck vuln DB unreachable (offline) | Substrate marked unavailable; never silently passes |
| Rule produces nondeterministic results | Exclude and log |
| Rule false-positive rate climbs | Demote severity in config (fingerprint-keyed) |
| Vuln exists on main | Baseline: reported, never blocks |
| apidiff delta without declared intent (or outside declared scope) | Fail gate, cite the diff |
| Findings in generated files / `walk:`-skipped paths | Filtered uniformly at the merge layer, all substrates |

## Decisions log

1. **(1a)** Doctor absorbs the finish gate. The finish/doctor split was the seam that let campaigns succeed on the weak bar.
2. **(2a, refined)** One rule set, package-scoped runs, persistent cache. The fast/full rule split from earlier drafts is dropped; react-doctor demonstrated diff scoping plus caching solves latency without rule tiers. Execution tiers (scope-capable vs full-run-only substrates) are an honesty statement about analyzer mechanics, not a rule split.
3. **(3b)** Category-derived severity; errors gate, warnings report. Blocking on dead code trains gate bypass.
4. **(4b)** No score initially. Counts and deltas provide the same gating power with nothing to calibrate. Score is a future presentation layer.
5. **(5a, refined)** Intent declarations resolve deliberate API changes; human confirmation would break unattended campaigns. Refined from a plan-schema field to a session-scoped, package/symbol-scoped intent record, because most mutations flow through direct CLI ops rather than orchestrator plans.
6. **(6a)** New findings only gate. Direct react-doctor precedent: diff mode fails on new regressions without dragging in baseline backlog.
7. Advisory-first rollout before the gate goes hard (borrowed from react-doctor's blocking:none default), with explicit journal-backed graduation criteria.
8. The existing structural linter is a first-class substrate, not deprecated. The "no bespoke rule engine" non-goal means no *third* engine — it does not retire the sensor half of the repo's harness.
9. Baseline marking reuses the lint fingerprint machinery with a per-base-SHA cached fingerprint set; the hot path analyzes one tree, never two.
10. Substrate unavailability is mode-dependent: advisory soft-skips loudly; gate mode records it and campaigns fail fast at start.
11. `doctor --fix` is kept; `FixCmd` hints on findings bridge detection to the existing verified autofixes and keep guide→sensor→autofix intact.
12. The Report/Finding structs are the specified shared contract (provenance, fingerprint, context, per-severity counts, schema version).
13. Findings are journaled to `.gorefactor/doctor-history.jsonl` (the failures-corpus pattern): the data source for both rule graduation and the prevention-loop metric.
14. Generated-file and `walk:` skip filtering is a merge-layer contract applied uniformly across substrates.

## Rule inventory: react-doctor adaptations

react-doctor's selection filter — code that compiles and passes tests but quietly misbehaves, especially agent-written code — applied to Go. Rules already covered by the structural linter or the golangci substrate are excluded; these are the gaps. Implemented rules live in the structural substrate (so they flow into `lint`, `doctor`, and `doctor --report` uniformly); each declares its `FixCmd` or documents why the fix is judgment-required.

| Rule | react-doctor analog | Category | Status |
|---|---|---|---|
| `vacuous-test` — a test with no assertion path (no `t.Error`/`Fatal`/`Skip`, `*testing.T` never passed to a helper) can never fail | the "agent writes bad code the gate can't see" premise itself | struct (gate integrity) | **Implemented.** Judgment-required fix by design |
| `sleep-in-test` — `time.Sleep` as synchronization in tests | flakiness family | struct (gate integrity) | **Implemented.** Judgment-required fix |
| `regexp-compile-in-func` — `regexp.MustCompile`/`Compile` with a constant pattern inside a function | `js-*` hoist RegExp/Intl | perf | **Implemented**, with `hoist-regexp` autofix (MustCompile sites; comment-preserving text surgery) |
| String `+=` in loops → `strings.Builder` | string concat in hot paths | perf | Planned (step 5) |
| Linear search inside a loop → build a map | use Set/Map for repeated lookups | perf | Planned (step 5) |
| Naked goroutines / `NewTicker` without `Stop` in library code | `effect-needs-cleanup` | conc | Planned (step 5; plan already commits to goroutine leaks) |
| Context misuse (`Background()`/`TODO()` where a ctx is in scope; loops without `ctx.Done()`) | lifecycle wiring family | conc | Planned — first try enabling golangci's `contextcheck`/`containedctx` via config + category mapping before writing our own |
| Pass-through parameters (forwarded ≥3 call layers unused) | prop drilling | struct | Planned (step 5; builds on existing callgraph infra) |
| `panic`/`log.Fatal` in library packages | server/client boundary violations | struct, shape-conditioned | Planned (step 4, needs shape detection; extends the plan's `os.Exit` example) |
| Dependency hygiene (`go mod tidy -diff` substrate; heavyweight single-symbol imports) | bundle size / barrel imports | dead | Planned (cheap substrate, step 4) |
| Sequential independent I/O → `errgroup` | fetch waterfalls | perf | Deferred — independence proof is the hardest analysis here |

Not transferable: the state-and-effects core (render-model-specific), accessibility, and their security rules (gosec's territory).

## Open items (empirical, not design)

1. **The load-bearing measurement**: warm-cache, package-scoped golangci-lint latency on a large production-scale codebase. The single-rule-set bet rests on this number being small (seconds). Measure before building the cache layer.
2. Temporal analyzer rule inventory: enumerate the workflow-determinism violations worth catching first, from the reference codebase's actual Temporal usage.
3. Reverse-dependency depth for scoping: depth-1 is the starting assumption; validate that depth-1 catches cross-package breakage apidiff misses.
4. Graduation threshold (initial N=50 clean runs) is a placeholder; tune against the findings journal once it has data.

## Build order

1. **Done.** `diagnose` API wrapping the structural linter + golangci-lint JSON, with fingerprint-based baseline marking against a cached base-ref set, findings journal, and the merge-layer skip filtering — implemented as the `doctor/` package (`doctor.Diagnose`), surfaced as `gorefactor doctor --report [--base REF] [--scoped]` (advisory: always exits zero; `--scoped` runs the same touched-packages-plus-reverse-deps scope as the agent gate). `FixCmd` hints already flow from the structural substrate's autofix commands.
2. **Done.** apidiff gate wiring on the existing differ + session-scoped intent records — `doctor.APIDiff` substrate (removed/changed gate as errors, additions warn) and `gorefactor intent api-change <scope> <reason>` / `--list` / `--clear` writing `.gorefactor/intents.json`; declared deltas demote to info citing the reason, scope-matched so blanket declarations don't pass.
3. **Done.** Gate wiring: the agent's `runGate` is build + test + scoped diagnose vs HEAD (golangci + apidiff substrates). Advisory-first per decision 7: the default `-doctor-gate advisory` reports new error-severity findings in the finish output without blocking; `-doctor-gate hard` blocks on them and on dark gating substrates, adds the campaign full-repo pass, and fail-fasts campaigns at start via `doctor.Preflight`.
4. govulncheck + deadcode (full-run tier) + shape detection
5. Temporal custom analyzers — first evaluate wrapping Temporal's official `workflowcheck` analyzer (`go.temporal.io/sdk/contrib/tools/workflowcheck`) as the substrate before writing our own, the same reuse-first logic as the context-misuse row (golangci's `contextcheck` before a bespoke rule)
6. Agent-install mode; complete the `FixCmd` mapping table for existing autofixes
7. Score layer (optional, last)
