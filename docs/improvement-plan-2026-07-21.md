# Improvement Plan — 2026-07-21

Follow-up to [project-review-2026-07-19.md](project-review-2026-07-19.md). That review's P0–P2
backlog is largely executed (engines extracted, one mutation path, I/O metadata contract,
types-aware rename, phantom targeting deleted, cost-of-pass measured). This plan covers what
remains, reordered by leverage. Theme: **the engineering debt is mostly paid; what's left is
credibility work** — zero-baseline self-cleanliness, external-repo numbers, and uniform
machine-readable output are what turn a self-aware project into a tool other people adopt.

Ground truth as of this writing (commit `0fd689b`):

- Baseline holds **89 suppressed findings**; top buckets: `long-function` 17, `complexity` 13,
  `data-clumps` 12, `high-blast-radius` 10, `test-only-live` 9. Sunset for an empty baseline:
  **2026-10-19** (`make gate-self-clean` exists but default `gate` still ratchets).
- `god-package` fires on `analyzer` (421 top-level decls / 54 files) and `cmd/gorefactor`
  (730 decls / 127 files).
- Envelope migration: 2 emitters on `emitEnvelope`; 8 files still hand-roll `--json` shapes.
- `doctor/score.go` self-describes as dashboard-only and uncalibrated; intents/apidiff have
  narrow consumers.
- All benchmark ratios were measured on this repo or its own corpus — none on foreign code.

Phases are ordered but 1, 2, 3 are independent of each other; 5 can start once Phase 3's
cross-check lands. Every phase ends with `make gate` green and, where findings shrink,
a `lint . --write-baseline` re-lock commit.

---

## Phase 0 — Quick wins (hours)

**0.1 `make build-fast`.** `build` front-loads test+lint+fmt+vet (~2 min). Add a compile-only
target for the edit-build-retry loop; keep `build` as-is for gating. Mention the fast path in
CLAUDE.md's build section.

**0.2 Triage the 9 `test-only-live` symbols.** Case-by-case, not a blanket delete:

| Symbol | Disposition |
|---|---|
| `analyzer.AnalyzeCrossFile` | Unexport or delete — no non-test consumer, no planned one |
| `analyzer.EmittableOperationTypes` | **Keep** — becomes the producer side of the Phase 3 op-registry cross-check |
| `analyzer.NewImportResolver` | Unexport or delete |
| `analyzer.FilterUsesByContext` | Unexport or delete |
| `IsDetailedError` / `AsDetailedError` (cmd) | Unexport — package main, export serves nothing |
| `doctor.ScoreClassifiedRules` | Resolve with Phase 2 (score layer decision) |
| `astcache.ResetIndexCaches`, `BuildCallIndexUncached` | Test helpers — unexport and move next to their tests |

**0.3 Reconcile live-lint vs gate-scope.** `lint .` reports 189 issues (incl. 101
`untested-function`) while the baseline tracks 89 — the difference is policy scoping that is
currently implicit. Document in `.gorefactor.yaml` comments which rules are advisory-only vs
gate-scoped, so "what does the gate check" has one written answer.

## Phase 1 — Finish the JSON envelope migration (1–2 days)

Migrate the 8 remaining bespoke `--json` emitters onto `emitEnvelope` (`{ok, error, data}`),
using `search-ast` as the reference shape: `cmd_api_diff.go`, `cmd_blast_radius.go`,
`cmd_callgraph.go`, `cmd_context.go`, `cmd_history.go`, `cmd_skeleton.go`, `cmd_txn_run.go`,
`mutation.go`. One command per commit; golden output tests per command.

Ratchet it: extend `registry_metadata_test.go` with an invariant that every command declaring
`--json` routes through the envelope, so new commands cannot regress. The consumer is a coding
agent parsing output — shape uniformity is a product feature, not polish.

## Phase 2 — Settle the doctor (1–2 days)

Apply the project's own standard — execution results outrank static claims — to the doctor's
speculative layers:

1. **Score**: delete `doctor/score.go` unless something gates on it by end of phase. It
   self-describes as uncalibrated and dashboard-only; no dashboard exists. (Resolves the
   `ScoreClassifiedRules` test-only-live finding for free.)
2. **Intents + apidiff**: wired only into the agent gate. Either wire into `doctor --report`
   (making the docs true) or cut from the library until a consumer exists. Recommendation:
   wire apidiff into `--report` (cheap, genuinely useful pre-release), cut intents.
3. **Pin the behavior**: add a doc-drift test asserting the documented stage list equals the
   actual stage list in `cmd_doctor.go`, so gate-vs-docs cannot silently diverge again.

## Phase 3 — Close the sensor loop (2–3 days)

The July review found twelve defect classes; sensors existed for one. Four shipped since
(`tracked-artifact`, `test-only-live`, `advertised-but-unwired`, `god-package`). Land the two
cheapest remaining, both as golden tests in the style of `lint_registry_test.go`:

1. **Op-registry cross-check** (the seam that actually broke — `rename_variable` was emitted
   with no executor): a test asserting `analyzer.EmittableOperationTypes()` ⊆ the set of op
   types `orchestrator` dispatches. Producer and consumer registries can never drift again.
2. **Behavioral doc-drift, scoped**: assert the README command survey matches the registry
   (count and names), and the doctor stage description matches Phase 2's pinned list. Do not
   attempt general prose-claim checking — pin the enumerable claims only.

Per the working agreement, each new sensor registers in the lint-registry golden test
deliberately; autofix only where a single safe transform exists (neither of these has one).

## Phase 4 — Baseline to zero (1–2 weeks; the big one)

Target: empty `.gorefactor-lint-baseline.json` and `gate-self-clean` becomes the default
`make gate`, well ahead of the 2026-10-19 sunset. Bucket the 89 findings three ways:

**4a. Real debt — fix.**

- **Split `analyzer/` (421 decls, 54 files).** The responsibility clusters are already visible
  in filenames: `analyzer/diff` (diff_analyzer*, diff_plan*, diff_patterns), `analyzer/metrics`
  (fn_metrics, complexity_reduction, length_reduction, dispatch_shape), `analyzer/callgraph`
  (call_*, dependency_graph), `analyzer/interfaces` (interface_*), detectors staying in the
  root or moving to `analyzer/detect`. Execute with gorefactor's own `move`/`txn` where the
  ops apply and **document the session as a case study** — this is the product demo doing the
  product's job. Note: `move`/`rename` are scoped to unexported symbols; exported-symbol moves
  across the new package boundary are gopls/manual territory — plan the split so most moves
  are whole-file, which sidesteps the limitation.
- **Shrink `cmd/gorefactor` (730 decls, 127 files).** Engines are already out; next largest
  coherent extractions are the lint rule implementations (→ `internal/lintrules`) and the MCP
  server (→ `internal/mcpserver`). Get under the god-package thresholds or, if a flat command
  registry is a deliberate choice, say so in policy config (4b) rather than baseline.
- Triage `high-blast-radius` (10), `linear-search-in-loop` (7), `pass-through-param` (6),
  `deep-nesting` (3), `duplicate-block` (3), `excessive-returns` (2), `high-coupling` (2),
  `untested-package` (2) individually; most are small, some fall out of the split for free.

**4b. Deliberately out of policy — configure visibly.** For `long-function` / `complexity` /
`data-clumps` findings that survive honest cleanup attempts: raise thresholds or exclude in
`.gorefactor.yaml` with a comment stating the rationale. A policy line is an honest statement;
a baseline entry looks like a suppressed bug. (Per the standing decision, duplicate-with-golangci
rules that own autofixes stay — this is about their thresholds on our own tree.)

**4c. Dead weight — delete.** Anything from 4a triage with no consumer and no thesis. The
repo's stated principle already decides these.

Exit criteria: baseline file empty and committed; `make gate` = `gate-self-clean`; the split
packages have no import cycles; full suite green; a short case-study doc from the analyzer
split (tokens spent, ops used, punts hit) added to `benchmark/`.

## Phase 5 — External validation benchmark (2–3 days)

Every published ratio was measured on the repo the tool was tuned on. Produce
`benchmark/EXTERNAL.md` with the same honesty FINDINGS.md applies internally:

1. Pick 2–3 foreign Go repos: one large and idiomatic, one messy/legacy-shaped, optionally one
   generics-heavy (the newest surface, likeliest to break AST assumptions).
2. **Sensors**: run `lint`, `skeleton`, `find-callers`, `find-uses`, `context`, `doctor --report`;
   record output-size ratios vs reading the files, plus every misfire (false positives on
   unfamiliar patterns) as a named finding.
3. **Guides**: on a scratch branch per repo, script a fixed battery of mutations (`rename`,
   `move`, `edit`, `extract`, `txn`) against real symbols; record success / clean-rejection /
   wrong-result counts. Wrong results are P0 bugs; clean rejections are the contract working.
4. Publish the numbers **including the negative results**, and feed every failure back as an
   issue or a new sensor candidate. This doc is simultaneously the adoption credential and the
   next bug corpus.

## Sequencing at a glance

| Phase | Effort | Depends on | Why this order |
|---|---|---|---|
| 0 Quick wins | hours | — | Unblocks loop speed; clears trivial findings |
| 1 Envelope | 1–2 d | — | Mechanical; independent |
| 2 Doctor | 1–2 d | — | Independent; resolves one Phase 0 symbol |
| 3 Sensors | 2–3 d | — | Cross-check protects Phase 4's big moves |
| 4 Baseline-zero | 1–2 w | 0,3 | The demo; largest and riskiest, do guarded |
| 5 External bench | 2–3 d | mostly 4 | Credential should show a self-clean tree |

Risks worth naming: the `analyzer` split can surface import cycles (mitigate: whole-file moves
first, `txn` batches, `undo` on failure); exported-symbol renames across new package boundaries
exceed `rename`'s documented scope (use gopls); Phase 4b threshold changes must not quietly
weaken the gate for consumers — policy changes get their own commits with rationale in the diff.
