# Project Review — Are We Building the Right Tool the Right Way?

**Date:** 2026-07-19. **Scope:** whole repository, reviewed from first principles, deliberately not
anchored on the existing CLAUDE.md/docs framing. Evidence gathered by four independent subsystem
audits (CLI, orchestrator/parser, agent/benchmark, analyzer/doctor/lint) plus the project's own
benchmark data. File:line references throughout are as of commit `c706bf6`.

---

## TL;DR verdict

**The core bet is right — but it is a much narrower bet than the codebase currently built around it.**

The validated product is: *deterministic, AST-safe edit primitives plus cheap repo sensors, consumed
by a host coding agent (Claude Code et al.) at ~zero marginal tokens*. The project's own benchmark
(`benchmark/FINDINGS.md`) proves this: `rename` via CLI costs ~0 tokens/33ms; `lint` saves 770× the
context of reading files; `find-uses` 259×.

Three large subsystems orbit that core without earning their keep:

1. **`gorefactor-agent`** (the LLM loop) — dominated on both sides by its own benchmark: the direct
   CLI beats it for scoped ops (0 vs 27K tokens for the same rename), Claude Code beats it for
   semantic changes. Its only defensible niche is campaign mode with a cheap local model, and the
   economic claim for that niche has never been measured.
2. **"Semantic targeting"** (the flagship innovation per README/docs) — roughly 40% phantom: five of
   the documented targeting fields have no code reading them, and the shipped examples rely on the
   phantom fields.
3. **Half the lint rule set** — duplicates or reinvents golangci-lint linters, including four rules
   that double linters *already enabled in this repo's own `.golangci.yml` at identical thresholds*.

Meanwhile the genuinely differentiated work — the extract-method and change-signature engines, the
autofix verify/bisect/outcome-journal pipeline, the harness-residue rules, the dry-run design — is
either locked inside `package main` where nobody can import it, or under-celebrated.

**Recommendation in one sentence:** reposition gorefactor as an *edit-and-sense toolkit for coding
agents* (CLI + MCP server, importable library underneath), cut or quarantine the agent binary and the
duplicate lint rules, fix the two unsafe transforms, and delete the phantom features.

---

## Context that shapes this review

- The entire ~68K-line project was built in a **three-day burst** (2026-07-17 → 07-19, 50 commits),
  largely agent-driven. Velocity was the point; this review is the consolidation step that velocity
  deferred.
- Build and tests are green (`go build ./...`, `go test ./...` all pass; full suite ~100s).
- The project keeps unusually honest self-assessments (`benchmark/FINDINGS.md`,
  `docs/harness-integrity-review-2026-07.md`). Much of this review simply takes those documents'
  conclusions more seriously than the codebase currently does.

---

## Part 1 — Are we building the right tool?

### 1.1 What the evidence validates

The project's own measurements define the value center precisely:

| Path | Cost | Verdict |
|---|---|---|
| Direct CLI (`rename`, `move`, `find-*`, `lint`) | ~0 tokens, ms latency | **This is the product** |
| Host agent (Claude Code) editing files directly | 8–16K tokens for the same rename | What the CLI displaces |
| `gorefactor-agent` driving the CLI via LLM | 20–75K tokens, ~45s | Dominated both sides |

(Three-way comparison and static ratios: `benchmark/FINDINGS.md`.)

The two properties buyers actually get:

- **Guides:** mutations parse the AST before writing, so the failure mode is "command rejects,"
  never "file silently breaks." This is real and consistently implemented.
- **Sensors:** `lint`/`doctor`/`find-*`/`skeleton`/`context` compress repo understanding into small,
  structured outputs. The 770×/259×/156× ratios are the honest headline.

### 1.2 The `gorefactor-agent` binary: dominated, per its own benchmark

`cmd/gorefactor-agent` (~50 files) reimplements a coding-agent harness: hand-rolled OpenAI and
Anthropic HTTP clients with retry/backoff (`provider.go`, `provider_anthropic*.go`), history
compaction and stale-output masking (`history_mask.go`), token budgeting, a 35-tool catalog, four
modes. It is competently built — and it is a re-implementation of what Claude Code, Cursor, and
aider already are.

Its own decision guide (`benchmark/FINDINGS.md`, "Decision guide") concludes: use the direct CLI for
scoped structural ops, use the host agent for semantic changes, and reserve `gorefactor-agent` only
for open-ended discovery and campaign cleanup. That leaves a narrow middle band, and the one
economically interesting claim in that band — *a cheap local junior (qwen-14B at 100% on solvable
task classes) saves frontier tokens on bulk cleanup* — has full measurement infrastructure
(`benchmark/sweep.go`, `pricing.go`, cost-of-pass matrix) but **no recorded frontier-vs-junior
dollar numbers**. The thesis is asserted, not proven.

Additional decay and risk in this subsystem:

- Hardcoded USD pricing table that self-admits it "WILL drift" (`benchmark/pricing.go:15-35`);
  hardcoded model IDs; `maxTokens` fixed at 4096 (`provider_anthropic.go:37`).
- Dead doc pointers: `triage.go` and FINDINGS.md cite three `RELIABILITY*.md` files that don't exist.
- A stale hardcoded commit trailer baked into campaign auto-commits (`campaign.go:191`).
- `gorefactorBin()` falls back to bare `"gorefactor"` on PATH (`agent_tools.go:267`) — in an
  autonomous flow, a poisoned PATH is an arbitrary-code-execution vector. Campaign mode auto-commits
  model-driven changes with no human gate and git-rollback-only isolation.
- The single-shot mode has drifted into a second-class path (7-op vocabulary vs the agentic 35-tool
  catalog; the failure-corpus feedback loop isn't wired into it).

### 1.3 "Semantic targeting": the flagship claim is partly phantom

README calls semantic targeting the key innovation. In fact (`orchestrator/targeting.go`,
`targeting_resolve.go`):

- `TargetSpecification.ControlStructures`, `Comments`, `BeforePattern`, `AfterPattern`,
  `SurroundingCode` (`orchestrator/types.go:31-42`) are **read by no scorer anywhere**. They are
  documented as working strategies (`ORCHESTRATION_SYSTEM.md:88-98`) and used by the shipped example
  `examples/multi_operation_plan.json` — where they are silently ignored.
- The scorers that do exist are heuristic: `scoreCodePattern` regex-matches pretty-printed source;
  `scoreVariableNames` counts any identifier with a matching *name string* (no scope/binding);
  `scoreFunctionCalls` only matches bare-ident calls, silently missing every `pkg.Func` / `x.Method`
  selector call — i.e. most real calls.
- The tie-breaking/ambiguity-error design (`targeting_resolve.go:121-142`) is genuinely good; the
  inputs feeding it are weak.

Separately, the orchestrator/CLI relationship is *better* than feared: the JSON-plan layer and the
direct commands share one engine (CLI `move`/`delete`/`rename` are thin fronts over plan operations;
`insert`/`replace` share `CodeInserter`). The plan format is a second front-end, not a second
engine. The dry-run design — run the real executor in a temp sandbox and diff
(`orchestrator/dry_run_execute.go`) — is a model worth preserving.

### 1.4 The lint system: differentiated core wrapped in golangci duplication

The lint system is AST-only, single-file, never type-checked (no rule touches `go/types` or
`packages.Load`). Overlap analysis against golangci-lint:

| gorefactor rule | golangci equivalent | Status |
|---|---|---|
| `complexity` (threshold 15) | `cyclop` max-complexity 15 — **enabled** | Exact duplicate, both running |
| `long-function` (75 lines) | `funlen` lines 75 — **enabled** | Exact duplicate, both running |
| `duplicate-block` | `dupl` — **enabled** | Overlapping intent, both running |
| `dead-code` | `unused` (type-aware) — **enabled** | `unused` strictly stronger |
| `funcorder-*` (~460 lines) | upstream `funcorder` — exists, not enabled | Reinvented |
| `error-not-wrapped` | `wrapcheck` — exists, not enabled | Reinvented, weaker (see 2.3) |

The project's own dogfood confirms the problem: the committed baseline suppresses **84 findings of
gorefactor's own linter on gorefactor's own code**, and the top two buckets (21 `long-function`,
17 `complexity` — 45% of the baseline) are exactly the golangci-duplicate rules. One baselined
`funcorder-function` finding has an autofix — a self-fixable violation was baselined instead of fixed.

What *is* differentiated and worth keeping: the harness-integrity family (`vacuous-test`,
`generated-name`, `byvalue-buffer`, `stranded-comment`, `orphaned-config-path`), the log-propagation
family with its autofix, `LogicLines`/dispatch-shape scoring (a real refinement over funlen/cyclop —
but it should be the *replacement* for the duplicate rules, not run alongside them), and above all
the **autofix pipeline**: batch + verify gate + bisect + outcome journal
(`cmd_lint_verify.go`, `.gorefactor/autofix-outcomes.jsonl`). No mainstream linter has
"execution results outrank static analysis" feedback. That is the moat; the rule-count is not.

### 1.5 The right tool, restated

> **gorefactor is an AST-safe edit-and-sense toolkit that makes any coding agent cheaper and safer
> on Go code.** Delivered as: (a) a deterministic CLI, (b) an MCP server exposing the same ops,
> (c) an importable Go library underneath. It is not an agent, not a general linter, and not a plan
> format.

Everything below follows from taking that sentence seriously.

---

## Part 2 — Are we building it the right way?

### 2.1 The engine is locked inside `package main`

`cmd/gorefactor` is a single flat `package main`: 128 non-test files, ~18.5K LOC — larger than
`analyzer/` or `orchestrator/`. 45 of those files import `go/ast`/`go/types`/`x/tools`; this is
engine code, not CLI glue. Concretely stranded and unimportable:

- The **extract-method engine** (~1,300 LOC: free-variable/liveness analysis, param/return
  inference, jump-barrier analysis — `cmd_extract*.go`).
- The **change-signature engine** (~620 LOC of cross-package call-site rewriting,
  `change_signature_*.go`).
- Parse/call-index caches with package-level singletons (`index_cache*.go`), the error-context
  machinery, diff rendering.

A third party — including our own MCP server story — cannot embed any of this. The split between
`analyzer/` (recommendation half) and `package main` (execution half of the *same feature*) is the
wrong seam.

### 2.2 Two mutation paths, two journals

`mutation.go` captures its own before-state and journals via `orchestrator.RecordOperation`, while
`runPlanOps` sets `orch.SkipSnapshot = true` to suppress the orchestrator's *other* snapshot
mechanism; a package-global `activeTxn` (`cmd_txn.go:58`) threads transactions through. Meanwhile
`extract` and `change-signature` bypass all of it — 25 files call `os.WriteFile` directly. Three
different places run build/test gates (`mutation.go:234`, orchestrator's test runner, `doctor/`).
Two undo systems over the same files is the most dangerous internal duplication in the repo.

### 2.3 Correctness gaps in shipped transforms and rules

- **`rename` is an AST-wide find-and-replace, not a rename.** `renameInFile`
  (`orchestrator/op_helpers.go:133-162`) rewrites every identifier whose name string matches — no
  `go/types` object identity, no scope. Shadowed locals, same-named fields, and unrelated
  identifiers in the directory all rename together. The guards (unexported-only, single dir,
  advisory `--strict`) are damage control around the wrong algorithm.
- **`error-not-wrapped` matches the literal spelling `err`** (`analyzer/error_wrap_detector.go:47`):
  `return e` / `return retErr` are missed; the `error`-return check matches only a bare ident named
  `error`. `wrapcheck` (type-aware) has none of these gaps.
- Name-based analysis is systemic: constructor detection by `New`/`Must` prefix
  (`analyzer/funcorder.go:220-241`), data-clumps keyed on AST-rendered type strings, dead-code by
  per-directory name reachability. Fine as advisory ranking; not fine where results drive autofixes
  or user trust.
- `insert_code` blindly type-asserts plan fields and can panic on malformed plans
  (`orchestrator/op_insert.go:24`).

### 2.4 Phantom, dead, and broken-by-construction inventory

- Five dead `TargetSpecification` fields (§1.3), advertised in docs and examples.
- `rename_variable` operations are **generated** by the diff analyzer
  (`analyzer/diff_analyzer_ops.go:222-228`) but **no executor dispatches that type** — plans
  produced from diffs containing a variable rename fail. (Known open item 11 in the integrity
  review.)
- `analyzer/call_hierarchy.go` and `call_chain.go`: zero non-test callers; kept alive only by their
  own tests. `cmd/gorefactor-test/` (a "Phase 4" black-box harness) is referenced by no Makefile,
  CI, or release config — compiled but never run.
- ~25-line blocks of stranded narration comments in `orchestrator/operations.go:28-73` and siblings
  — the exact smell the project's own new `stranded-comment` linter targets.
- Git-tracked build artifacts: `gorefactor-phase2` (11.5MB Mach-O), `phase4-test` (3.4MB),
  `orchestrator/gorefactor` (915KB), `coverage.html` (530KB — listed in `.gitignore` yet still
  tracked).

### 2.5 Two doctors, one of which is mostly speculative

- `doctor` (the gate) runs a lint stage of **only 3 checks** — file-size, duplicates,
  untested-packages (`cmd_doctor.go:165-170`) — not the 41-rule registry, while the docs describe it
  as "structural lint + …". The load-bearing gate and the documentation disagree.
- `doctor --report` (the `doctor/` package) carries substrates, fingerprinting, tiering, scoring,
  intents, apidiff. Of these: the score is self-admittedly uncalibrated and nothing gates on it
  (`doctor/score.go:8-10`); apidiff + the intent system are wired only into the agent's gate, never
  into `--report` — yet `doctor install` *claims* `--report` includes apidiff
  (`cmd_doctor_install.go:85`); the `.gorefactor.yaml` tier system has full machinery and no config
  file in existence. The substrate abstraction itself (compose golangci/govulncheck/deadcode with an
  honest `ErrUnavailable` distinction) is good; the superstructure is mostly latent.
- The baseline fingerprint composition is copy-pasted between `doctor/fingerprint.go:33` and
  `cmd_lint_baseline.go:43`.

### 2.6 CLI framework and I/O consistency

The homegrown registry (`registry.go`) buys one real thing — MCP tool schemas derived from command
metadata — but costs: stringly-typed flags parsed twice by two implementations that must agree
(`checkCommandArgs` vs `parseFlags`), ~15 files still hand-rolling flag loops, ~16 bespoke `--json`
shapes with no shared envelope, a swallowed encode error in `emitJSON`, hand-maintained usage
strings, and **four parallel allowlists** (MCP read-only/write/idempotent, txn) that must be
manually synced when a command is added. `os.Exit(0)` inside a command handler (`cmd_repl.go:40`)
bypasses the otherwise-good semantic exit-code contract.

### 2.7 Documentation and config sprawl

~4,000 lines of instruction/docs across 13 files, including an 855-line CLAUDE.md that embeds a
63-command reference duplicating README and `--help`. Instruction text outweighs several of the
packages it governs, and it has already drifted (doctor's lint stage, apidiff claim, semantic
targeting). Config surface: `.golangci.yml`, baseline JSON, `.goreleaser.yaml`, `opencode.json`,
AGENTS.md, plus machinery for a `.gorefactor.yaml` that doesn't exist.

---

## Part 2.5 — The dogfooding test: would gorefactor have caught its own defects?

gorefactor exists to keep *other* Go projects clean. The hardest and fairest benchmark it will
ever face is therefore its own repository — a 68K-line, agent-built-at-speed codebase, i.e. exactly
the kind of project the tool targets. So score it: **of the defect classes this review found, how
many would any of the 41 sensors have flagged?**

| Defect found in this repo | Caught by a sensor? | Why not |
|---|---|---|
| Committed binaries + tracked `coverage.html` (~16.5MB) | **No** | No repo-hygiene rule; sensors only walk `.go` ASTs |
| Five phantom targeting fields, advertised in docs and examples | **No** | No "advertised-but-unwired" sensor; `dead-code` doesn't cover struct fields |
| `rename` is scope-blind find-and-replace | **No** | Semantic correctness — the integrity review already names this blind spot |
| Diff analyzer emits `rename_variable` ops no executor dispatches | **No** | No producer/consumer registry cross-check (the lint registry *has* one; the op registry doesn't) |
| Dead `call_hierarchy.go`/`call_chain.go` | **No** | Their own tests reference them, and test references count as "uses" |
| `cmd/gorefactor-test/` compiled but wired into nothing | **No** | Nothing senses "built but absent from Makefile/CI/release" |
| 25 lines of stranded narration comments in `orchestrator/operations.go` | **No** — verified | `stranded-comment` matches mis-attached *doc* comments; free-floating comment groups don't fit its pattern |
| Doctor gate runs 3 rules while docs claim the full lint | **No** | Doc-drift tests pinned constants in CLAUDE.md only; behavioral claims unsensed |
| README said 28 rules (41), 24 steps (40), 6 commands undocumented | **Partially** | The drift-test *class* exists but watched the wrong file |
| 128-file `package main`, engines unimportable | **No** | `file-size` is per-file; `god-object`/`large-class` are per-type; nothing is per-package |
| Two journal/undo systems, duplicated gates | **No** | `duplicate-block` sees textual clones, not conceptual duplication |
| 84-finding self-baseline incl. an autofixable `funcorder` violation | **Sensor fired** | …and the finding was baselined instead of fixed |

Rounded generously: the sensors caught **one of twelve** defect classes, and the culture suppressed
that one. Meanwhile the rules that dominate the self-baseline (`long-function`, `complexity`) are
proxy metrics the doctor score itself half-weights as gameable-by-code-motion — the tool is
strictest about the things that matter least, and blind to the things that actually degraded it.

Two conclusions follow:

1. **This repo is the product demo, and the demo currently argues against the product.** The first
   thing a prospective adopter will do is run gorefactor on gorefactor. Today that returns "no
   issues" over a baseline hiding 84 findings, in a tree containing committed binaries, phantom
   features, and dead subsystems. The baseline/ratchet was designed for adopting *legacy*
   codebases; using it to suppress the tool's own findings on a three-day-old repo is a misuse of
   the mechanism, and "self-clean without a baseline" should be a release gate.
2. **This review is a free corpus of next-generation sensors.** The defects above are precisely the
   failure modes of agent-built codebases at speed — speculative subsystems, phantom surface area,
   doc drift, conceptual duplication — which is the exact market the tool serves, and no current
   rule targets them. The project's own harness rule ("every new capability gets a sensor") applied
   to itself says: each row in the table becomes either a new sensor or an explicit out-of-scope
   note. Concretely promising: `tracked-artifact` (binary/coverage committed to git),
   `test-only-live` (exported symbol referenced only from `_test.go`), free-floating
   stranded-comment detection, `advertised-but-unwired` (docs/examples referencing flags, fields,
   or op types nothing reads — the generalization of `orphaned-config-path`, which is the right
   instinct scoped too narrowly), a per-package god-package rule, and an op-registry cross-check
   test mirroring `lint_registry_test.go`.

---

## Part 3 — Recommendations

Ordered; each is independently shippable. P0 is hygiene, P1 is strategy, P2 is architecture, P3 is
prove-or-cut experiments.

### P0 — Hygiene (hours, do immediately)

1. `git rm` the tracked binaries (`gorefactor-phase2`, `phase4-test`, `orchestrator/gorefactor`) and
   `coverage.html`; tighten `.gitignore`.
2. Delete `cmd/gorefactor-test/` (or wire it into CI as a real smoke stage — but pick one; today it
   is compiled dead weight with a misleading name).
3. Sweep the stranded comments in `orchestrator/` with the project's own `stranded-comment` rule.
4. Fix the false claims: doctor-install's apidiff line (`cmd_doctor_install.go:85`), the dangling
   `RELIABILITY*.md` references, the doctor-gate lint description in docs.
5. Remove the hardcoded stale commit trailer in `campaign.go:191`; make `gorefactorBin()` require a
   resolved absolute path.

### P1 — Strategic cuts (days)

6. **Retire the phantom targeting fields** — delete the five dead `TargetSpecification` fields from
   types, docs, and examples (or implement scorers; deleting is recommended — nothing has missed
   them). Fix or stop emitting `rename_variable`.
7. **Put the lint rule set on a diet.** Delete `complexity`, `long-function`, `duplicate-block`,
   `dead-code` as bespoke rules (golangci's cyclop/funlen/dupl/unused are already enabled in this
   repo); enable upstream `funcorder` and `wrapcheck` and delete the ~460-line reimplementation and
   the spelled-"err" detector. Keep the differentiated families (harness-residue, log-propagation,
   test-hygiene, conc/lifecycle) and keep `LogicLines`/dispatch-shape as the *replacement*
   refinement only if routed through one engine. Then rewrite the baseline — after this diet it
   should approach empty, which is the honest state.
8. **Decide the agent binary's fate: campaign-or-cut.** Keep campaign mode (the one niche with a
   plausible thesis) plus the triage fast-path; delete single-shot and interactive modes and the
   second planner vocabulary. Better: replace the hand-rolled provider clients with official SDKs or
   fold campaign mode into a docs recipe driving Claude Code + the MCP server. Do not keep
   maintaining a general-purpose agent harness.
9. **Unify on one doctor.** Make the gate and `--report` share one engine and one honest
   description; delete the score layer, intents, and apidiff from the library until something
   consumes them (they can return with a consumer).

### P2 — Architecture (1–2 weeks, mechanical)

10. **Extract the engines from `package main`** into importable packages: `refactor/extract`,
    `refactor/changesig`, `internal/astcache`, `internal/cli`. This is the single highest-value
    structural change — ~2K LOC of the best code in the repo becomes usable by the MCP server, the
    library API, and third parties. No import cycles exist; it is a mechanical, multi-day move.
11. **One mutation path.** Route every mutator through the orchestrator's operation/journal/snapshot
    path; delete `SkipSnapshot`, `activeTxn`, the duplicate gate runners, and the double
    journaling. One undo system.
12. **One I/O contract.** Single flag parser, single `--json` envelope (`{ok, error, data}`),
    registry metadata (`ReadOnly`/`Mutates` on `Command`) generating the MCP/txn allowlists instead
    of four hand-synced lists.
13. **Fix `rename` properly**: bind identifiers via `go/types` object identity (the repo already
    does this elsewhere) or delegate to gopls. The current algorithm should not ship under the name
    "rename."

### P3 — Prove or cut (ongoing)

13a. **Adopt "self-clean, no baseline" as the release gate**, and turn Part 2.5's table into the
    sensor backlog: each defect class this review found gets a sensor or a documented
    out-of-scope decision. The repo is the benchmark; a tool that keeps other projects clean
    must demonstrably keep itself clean first.

14. **Run the cost-of-pass sweep and publish frontier-vs-junior numbers.** The campaign-mode
    economic thesis is the project's most novel claim and its infrastructure is already built. If
    the numbers are good, that's the headline; if not, cut the binary to the triage fast-path.
15. **Keep the honest-measurement culture** (FINDINGS.md, integrity reviews) and extend it: the
    `recommend` 0.7× negative result was caught *because* the project measures itself. Every new
    surface should ship with its ratio.

### What to protect (explicitly not broken)

- The dry-run sandbox design (`orchestrator/dry_run_execute.go`) — real executor in a temp copy,
  then diff. Model for all future previews.
- The autofix verify → bisect → outcome-journal loop, and "execution results outrank static
  analysis" as a principle.
- The semantic exit-code contract and structured punt reports.
- The one-way baseline ratchet with visible `[baseline-growth]` opt-in.
- Doc-drift tests pinning constants to prose, and the golden rule-registry test.
- `benchmark/FINDINGS.md`'s willingness to record negative results.

---

## Closing

The three-day build produced a real product plus two speculative products wrapped around it. The
real one — deterministic guides, cheap sensors, a gate that catches what the heuristics miss — is
validated by the project's own data and deserves the focus. The kindest thing to do for the
speculative parts is to subject them to the same standard the project already applies to its lint
findings: **execution results outrank static claims**. Measure campaign mode or cut it; implement
semantic targeting or delete it; and let golangci do the linting it already does, so gorefactor's
sensors can be the ones nobody else has.
