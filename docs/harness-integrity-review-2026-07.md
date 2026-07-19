# Harness integrity review — findings, learnings, and follow-up plan (2026-07)

## Context

After a score-improvement campaign raised `doctor --report --score` from 36.5
to 72.5, we ran a deliberate *no-tools* review of this repository: three
independent read-throughs (CLI package; analyzer/orchestrator/parser;
agent/doctor/benchmark/docs) judging the code the way a human reviewer would,
specifically looking for what the sensors cannot see and for scars the
harness itself may have left. This document records what the review found,
what changed because of it, the durable lessons, and the plan for the work
not yet done.

The one-line verdict: **the harness was working, insufficient, and
overbearing at the same time — in different places** — and each of those
three words pointed at a different class of fix.

## What the review found

**Working.** The hand-designed spine is genuinely good: the command
registry, the semantic exit-code error model, the unified `mutation` runner,
journal/undo, semantic targeting, and the doctor substrate contract (a dark
*gating* sensor is a gate failure, never a pass). The best-designed
subsystems were also the best-tested.

**Overbearing.** A historical bulk run of `doctor --fix --fix-level
aggressive` (commit `edb6cb3`) strafed the tree with mechanical residue:
~35 `extractBlockL<line>` helpers (including a 16-value flag tuple and a
12-parameter lifted closure), `(r0, done bool)` sentinel returns, stutter
filenames (`orchestrator_orchestrator.go`,
`provider_anthropic_anthropic_provider.go`), a comment-corrupted
`orchestrator/types.go` — and one **shipping bug**: an extraction converted
a captured `bytes.Buffer` into a by-value parameter, so `add-test` generated
scaffolds whose `t.Run` bodies were silently empty. It passed the gate for
months because the scaffold tests asserted substrings, not behavior.

**Insufficient.** The worst code carried zero findings: `parser/` silently
dropped grouped parameters and lossy-stringified types; the rename validator
returned confident booleans from bare name-matching; the diff-hunk parser
collapsed multi-edit hunks; four AST-stringifiers coexisted at incompatible
quality levels; CLAUDE.md contradicted itself about its own rule count.

## What changed because of it (all landed)

- The `add-test` lost-write bug fixed, with a regression test that asserts
  the generated body actually invokes the function under test.
- All 35 residue helpers re-inlined with natural control flow; `types.go`
  reconstructed; stranded comments removed; 15 stutter files renamed.
- `parser/` made truthful: one `Param` per name, `types.ExprString`,
  embedded interfaces, tests that pin correct behavior.
- Two new sensors: `generated-name` (committed extractor-fallback names)
  and `byvalue-buffer` (`bytes.Buffer`/`strings.Builder` by value).
- Doc-drift guard: every "N rules" claim in CLAUDE.md must equal the
  registry size (`TestDocDrift_RuleCountMatches`).
- Earlier in the same arc, and motivated by the same evidence: the
  value-tiered score, dispatch-table per-branch normalization, the
  vacuous-extraction guard, the collision-suffix nameability gate, and the
  autofix outcome journal with `--probe-fixes`.

**Honest accounting:** completing the cleanup *lowered* the health score
(72.5 → 64.7) because un-chopping fake helpers let ~20 real
`long-function`/`complexity` findings resurface. They were never fixed,
only hidden. The baseline was re-locked with the visible
`[baseline-growth]` marker. A metric you can trust more after it drops is
the goal, not a regression.

## Durable lessons

1. **Function-total metrics charge tables per row; readers pay per row
   read.** A dispatch table is read one case at a time, so lines/cyclomatic/
   cognitive all over-charge it linearly in case count. Fix ratios, not
   thresholds: score per independent branch (implemented as
   `analyzer.AnalyzeDispatch`).
2. **Any proxy metric can be satisfied by code motion.** Wrapping a body in
   one helper clears `long-function` without improving anything. Weight
   proxies below defects in the score, and make the *guides* refuse vacuous
   transforms — pressure without a refusal path produces churn.
3. **Execution results outrank static analysis.** A gate-reverted dead-code
   deletion *falsifies* the finding; a gate-reverted error-wrap says the
   error text is contractual. Journal outcomes and feed them back
   (implemented as the autofix outcome journal + `--probe-fixes`).
4. **The gate is exactly as strong as the tests beneath it.** The buffer
   bug compiled, and substring tests blessed empty output. Structural rules
   cannot measure "this test asserts too little". Mitigation: every code
   *generator* gets behavioral/golden tests (its output must be exercised,
   not pattern-matched). See plan item 4.
5. **Mechanical residue compounds and must be handled twice**: blocked at
   generation time (nameability + vacuity gates in the extractor) *and*
   detected in the tree (`generated-name`), because history happened before
   the gates existed.
6. **Renames silently orphan path-scoped config.** Renaming
   `dry_run_orchestrator.go` broke a path-anchored golangci exemption; we
   only noticed because the un-exempted linter fired. Config that points at
   paths needs a liveness check. See plan item 3.
7. **Docs drift unless a test pins them to the code.** The same file
   claimed 25 and 37 rules simultaneously. Every count or default stated in
   prose should be derivable — or asserted — from the code (the rule-count
   guard now does this; the iteration-default claim is next, plan item 5).
8. **Zero findings is not evidence of quality.** `parser/` was the least
   correct package in the repo and the sensors had nothing to say about it.
   Periodic no-tools review is a scheduled activity, not a one-off (plan
   item 7).

## Plan for the remaining work

Ordered by leverage; each item lists the deliverable and its acceptance
criterion. Per the harness pattern, detection work ships as sensor + (where
a single safe transform exists) autofix.

1. **`stranded-comment` sensor.** A doc comment whose leading identifier
   names a *different* top-level declaration in the package than the one it
   precedes (the exact `types.go` / `orchestrator.go` failure mode).
   Heuristic is precise: first word of the comment block is a Go identifier,
   ≠ the following decl's name, and == some other top-level decl's name.
   Severity: warning. Acceptance: fires on the pre-repair `types.go` as a
   fixture; zero findings on the current tree.
2. **Fix `split` naming at the source (guide fix).** The splitter mints
   `<parent-stem>_<receiver-or-prefix>` names, producing
   `provider_anthropic_anthropic_provider.go`-style stutter. Deduplicate
   overlapping stem tokens when composing the new filename. Acceptance:
   splitting a file named after its dominant receiver never repeats a token;
   add a unit test with the provider fixture. (The sensor half is optional —
   a stutter-filename check is cheap but cosmetic; the guide fix is the real
   one.)
3. **Config-path liveness check.** A lint rule (or doctor substrate) that
   parses `.golangci.yml` path regexes and the lint baseline file list, and
   warns when an entry matches zero files in the tree — orphaned exemptions
   are silent scope creep in reverse. Acceptance: renaming an exempted file
   without updating the config produces a finding in the same commit.
4. **Behavioral tests for every generator.** `add-test` is done. Same
   treatment for the remaining output-producing commands:
   `extract-interface`, `implement-interface`, `init-agent-rules`,
   `doctor install`, `generate-templates`, and the plan-template emitters —
   each gets at least one test that *exercises or compiles* the generated
   artifact rather than substring-matching it. Acceptance: for each
   generator, deleting the core of its output makes a test fail.
5. **Extend the doc-drift guard** beyond rule counts: assert the agentic
   iteration default and autofix batch size stated in CLAUDE.md against
   their constants (same pattern as `TestDocDrift_RuleCountMatches`).
6. **Consolidate the remaining stringifiers.** `parser.exprToString` now
   delegates to `types.ExprString`; migrate `analyzer.exprString` (returns
   `""` for non-idents) and evaluate `orchestrator.nodeToString` /
   `api_diff`'s `render` for consolidation. Acceptance: one blessed
   implementation, others delegate or are deleted; `find-uses` shows no
   remaining callers of the lossy forms.
7. **Soundness honesty for the rename validator.** Either implement
   scope-aware reference finding (`go/types`-backed, like `find-uses`) or
   rename the API so it stops promising what it cannot deliver
   (`SafeToRename` → advisory hints with an explicit "name-match only"
   caveat). Undecided which; decide at implementation time, but the current
   confident-boolean surface must not survive.
8. **Diff-hunk fidelity.** `analyzeHunk` keeps only the first modification
   per hunk and discards old-side ranges. Fix to emit one change per
   `-`/`+` run. Acceptance: a two-edit hunk fixture yields two changes.
9. **Decide the fate of `sweagent/` and `.pi/`.** Both are inert (zero Go
   references). Either delete them or add a top-level note declaring them
   experiments; unlabeled dead scaffolding reads as rot. Owner's call.
10. **Schedule the no-tools review.** Once per release cycle, run the
    three-reader review against the current tree and append a dated section
    here. The sensors' blind spots (lesson 8) make this the only detection
    path for a whole class of defect.

## Addendum — 2026-07-18 dogfood loop

A `/goal` dogfood pass surfaced a lesson worth pinning as **lesson 9**:

9. **A *scored* substrate that silently skips makes the score lie.** The
   `deadcode` binary was not on PATH, so `doctor --report --score` reported
   64.7 while omitting 24 real dead-code findings — the number read healthier
   than the tree was, with the skip visible only in a `[deadcode] skipped`
   line most readers scan past. Fix is two-sided, mirroring the golangci
   precedent: **prevention** — `.claude/hooks/session-start.sh` best-effort
   `go install`s `deadcode` so the local score is complete; **honesty** —
   `printDoctorReport` now appends "N scored substrate(s) skipped (…) — score
   is optimistic" whenever any non-`baseline` substrate did not run
   (`scoredSubstratesSkipped`, pinned by test), so even a network-blocked
   checkout cannot present a partial score as a whole one. Same shape as
   lesson 3 (dark gating substrate) but for the *presentation* score rather
   than the gate.

Also actioned in the same pass: 17 genuinely-dead symbols removed (honest
score 49.4 → 59.3, again *below* the inflated 64.7 — cleanup lowering a
now-trustworthy number, lesson from the header). The removal exposed a
compiler-and-linter-defeating scar worth noting: a package-level
`var _ = deadFunc` blank-identifier reference keeps an otherwise-dead helper
"used" for `go build` *and* hides it from gorefactor's own `dead-code` rule;
only the whole-program `deadcode` substrate saw through it. Candidate future
sensor if the pattern recurs (it was a one-off here).

## Addendum — 2026-07-19 plan execution

Items 1–8 implemented in one pass (one commit per item on the
`claude/harness-integrity-review-2026-07-qx51nl` branch line). Notes beyond
the plan text, in the spirit of honest accounting:

- **The new sensors found real defects on their first run.**
  `stranded-comment` (item 1) surfaced two genuinely stranded doc comments
  (`dispatchTool`'s doc sitting on `runGateWithAdvisory`; `chatPause`'s doc
  sitting on `const pausePrompt`) — both re-homed in the same commit.
  `orphaned-config-path` (item 3) surfaced two dead golangci exemptions
  (`^testfiles/`, `^move_by_plan\.go$` — neither path exists) — removed,
  golangci confirmed clean without them.
- **Item 4's behavioral tests exposed a shipping capability lie**: the
  template generator (and CLAUDE.md, and `analyze-diff` plans) advertised
  `extract_method` / `inline_method` / `rename_variable` plan operations
  that the orchestrator rejected as `unknown operation type` — the tool's
  own `generate-templates` output failed its own `orchestrate`. Same class
  as lesson 8: zero findings, worst behavior. Fixed by an
  external-handler registry (`orchestrator.RegisterExternalHandler` /
  `KnownOperationTypes`, dispatch-probe-pinned) with `cmd/gorefactor`
  bridging `extract_method` and `inline_method` to its existing engines —
  both now execute end-to-end from a JSON plan under compile-verified
  tests. `rename_variable` had no engine anywhere and is gone from
  templates and docs (replaced by the executable `rename_declaration`).
- **Item 7** took the honesty option: `RenameAdvisor`/`RenameHints` with
  `Blocking`/`Advisory` hint lists (plus a new package-level-collision
  check) replaced the `SafeToRename` boolean; the report renders no
  verdict and states the name-match-only limit.
- **Item 6** evaluation result: `orchestrator.nodeToString` and
  `api_diff`'s `render` stay — both are lossless `format.Node`-based
  renderers of whole nodes, which `types.ExprString` cannot replace. The
  lossy `analyzer.exprString` is deleted; `types.ExprString` is the single
  blessed type-expression stringifier.

**New item from this pass:**

11. **`analyze-diff` still emits `rename_variable` operations**
    (`createRenameVariableOperation` in `analyzer/diff_analyzer_ops.go`),
    which no executor dispatches. Either implement a function-scoped
    variable-rename executor (conservative: ident match within the resolved
    function body) or emit an advisory change without an operation. Until
    then, plans generated from diffs containing variable renames fail at
    that op with a clear error rather than silently — acceptable, but
    dishonest to leave advertised.

## Status

Items 1–8: **done** (2026-07-19 addendum above; one commit per item).
Item 9: **done** — owner chose deletion; `sweagent/`, `.pi/`, and the
`.pi`-describing `PI_INTEGRATION.md` are removed (zero Go references, so
no code impact). Item 10 (schedule the no-tools review) remains open — an
owner process commitment, not code. Item 11 (new, above) is not started. Everything in "What changed because of it" is merged
into the PR #52 branch line. The 2026-07-18 addendum items (score-skip
honesty + prevention) are done.
