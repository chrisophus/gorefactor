# Improvement Brainstorm — July 2026

Grounded in a fresh pass over the repo (the previous plan in
[gorefactor-improvements-plan.md](gorefactor-improvements-plan.md) has shipped
all eight of its items). Ideas are grouped by theme; each theme is roughly
ordered by impact-per-effort. A shortlist of recommended next steps is at the
end.

## Observed facts driving this list

- **The gate passes locally but is not enforced in CI.** `gorefactor doctor`
  passes on this repo (lint: 0 error-severity issues) via the *optional*
  pre-commit hook, but CI (`.github/workflows/ci.yml`) runs
  vet/golangci-lint/tests and never runs `gorefactor lint` or `doctor` — so
  the project's headline gate can regress silently for anyone without the
  hook installed (and `git commit --no-verify` bypasses it entirely).
- **A sizeable advisory backlog**: `lint .` reports **134 warnings** (54
  `long-function`, 32 `duplicate-block`, 16 `complexity`, 11 `deep-nesting`,
  …) plus 578 hidden `[info]` findings. None block, none ratchet.
- **Committed build artifacts**: `coverage.html`, a Mach-O binary
  `gorefactor-phase2`, and `phase4-test` are tracked in git.
- **The previous improvement plan is 100% shipped** and reads as historical
  record rather than a live roadmap.

## A. Dogfooding & adoption (highest leverage)

1. **Run `gorefactor doctor` in CI.** The gate currently lives only in the
   optional local pre-commit hook. Adding a doctor step to `ci.yml` makes the
   error tier a hard ratchet on every PR (and, unlike the local hook, CI has
   the pinned golangci-lint, so that stage actually runs there).

2. **Baseline / ratchet mode: `lint --baseline`.** Snapshot current findings
   (à la golangci-lint's `new-from-rev`), then fail only on *new or worsened*
   issues. Two payoffs: (a) adoption on existing codebases — day one on a
   large repo is a wall of findings with no incremental path; (b) this repo's
   own 134-warning backlog becomes enforceable ("no new warnings") without
   first paying it all down.

3. **Empirically calibrate default thresholds.** The `benchmark/` corpus
   miner already exists — reuse it to sweep rule thresholds across popular Go
   repos and pick defaults that flag roughly the worst decile rather than the
   median. Publish the calibration data in docs so the defaults are
   defensible, and consider shipping the result as a named `--profile`.

4. **Doc-drift sensor.** CLAUDE.md and README carry a large hand-maintained
   command table; `getCommands()` is the source of truth. Add a test (or lint
   rule) that diffs registered command names/flags against the documented
   table, failing on drift. Optionally generate the table (`gorefactor
   docs --markdown`) instead of hand-writing it.

5. **Repo hygiene.** Delete and gitignore `coverage.html`,
   `gorefactor-phase2` (a committed Mach-O binary), and `phase4-test`. Archive
   the fully-shipped improvements plan under a "done" heading so the live
   roadmap is discoverable.

## B. New guides (mutation commands)

6. **`extract-var` / `extract-const`** — extract an expression into a named
   variable or constant. One of the most common day-to-day refactors and a
   natural complement to `extract` (statements) and `inline`.

7. **Exported-symbol rename.** `rename` currently punts exported symbols to
   gopls. An in-process module-wide rename via `go/packages` closes the gap
   for the common case (single module, no downstream consumers), with a
   `--force` flag acknowledging the API break — pairs naturally with
   `api-diff` output.

8. **Cross-package `move`.** `move` handles file-to-file within a package;
   moving a declaration to another package (qualifier rewriting at call
   sites, import updates, export-case adjustments) is the harder,
   higher-value version and what the Decision Matrix already advertises.

9. **`change-signature` extensions**: `--reorder-params`, and
   `--add-return`/`--remove-return` (updating call sites that discard or
   capture the value). Also `--add-param` with a context.Context fast path
   (`--add-ctx`) since threading a context is the canonical signature change.

10. **Exhaustive-switch autofix.** For a `switch` over a const-enum type, add
    the missing `case` arms as stubs. Guide + sensor pair (see #13).

## C. New sensors (lint rules)

11. **Cyclic / layered import rule.** `find-package-deps` already detects
    circular imports; surface it as a lint rule so it participates in
    `doctor` without go-arch-lint installed.

12. **Context hygiene rules**: `context.Context` must be the first parameter;
    flag `context.Background()`/`TODO()` in non-main, non-test code.

13. **`inexhaustive-switch`** (pairs with autofix #10): switch over an
    enum-like const set missing cases and lacking `default`.

14. **Test-quality rules**: tests that assert nothing, table-driven tests
    without `t.Run`, missing `t.Parallel` (advisory tier). Complements the
    existing `untested-*` coverage rules with a quality dimension.

## D. Agent & harness

15. **Mine the failure corpus back into the prompt.** `.gorefactor/failures.jsonl`
    is currently a passive sensor. Aggregate it (top rejected op shapes, per-rule
    autofix revert rates) and inject a short "known failure modes" section into
    the agent system prompt — closing the mistake-cannot-recur loop that the
    harness docs describe but don't yet implement end-to-end.

16. **Named checkpoints for undo.** `undo` rolls back the last unit;
    `gorefactor checkpoint <name>` + `undo --to <name>` would let an agent
    bracket a whole campaign and bail out cleanly, instead of unwinding op by
    op.

17. **Expose `orchestrate` over MCP** — the one deliberately-deferred item
    from the previous plan. Bound it: validate the plan against the op schema,
    cap op count, require clean worktree, and reuse the txn/undo journal so a
    remote plan is still one undo unit.

18. **`lint --fix --dry-run`.** Autofix currently applies-then-verifies;
    a preview mode (unified diff of what *would* be applied) makes `--fix`
    usable in review workflows and from the MCP read-only surface.

19. **Token-cost regression benchmark in CI.** `benchmark/` already has a
    sweep harness and FINDINGS.md. Wire a small, deterministic subset into CI
    (or a scheduled workflow) so changes to prompts/masking/tool surfaces
    show their token-cost delta in the PR, the same way perf suites do.

## E. Performance & UX

20. **Parallelize lint rule execution.** Profiling flags (`--cpuprofile`,
    `--profile-rules`) exist; rules are largely independent per-file walks and
    a `packages.Load` — fan out across cores, dedupe the load. Doctor runs it
    on every gate, so latency here is paid constantly.

21. **`gorefactor watch`** — re-run `lint` (fast, in-process) on file save,
    printing only deltas. Cheap to build on the existing walker; makes the
    sensors ambient during human editing, not just agent editing.

22. **Multi-step `undo` / `redo`.** `history` already journals every mutation;
    `undo` should accept a count or op-id (`undo --last 3`, `undo --to <id>`)
    with `redo` as the inverse, so recovery doesn't require git.

## Recommended shortlist

If picking five, in order:

1. **#1 doctor in CI** — cheap, and turns the existing green gate into a real
   ratchet instead of an optional local hook.
2. **#2 `lint --baseline`** — the biggest adoption unlock for external repos,
   and makes this repo's warning backlog enforceable incrementally.
3. **#4 doc-drift sensor** — cheap, and the command table is the product's
   primary interface for LLMs.
4. **#6 `extract-var`/`extract-const`** — highest-frequency missing guide.
5. **#15 failure-corpus feedback** — completes a loop the harness docs
   already promise, using data the tool already collects.

(#5 repo hygiene is a 10-minute cleanup that can ride along with any of the
above.)
