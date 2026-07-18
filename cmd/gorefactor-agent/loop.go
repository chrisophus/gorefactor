package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/chrisophus/gorefactor/analyzer"
	"github.com/chrisophus/gorefactor/orchestrator"
)

// Config holds one driver run's parameters.
type Config struct {
	Spec       string    // purified refactoring spec (from crucible, etc.)
	Dir        string    // target Go module directory
	MaxIter    int       // attempt cap
	DryRun     bool      // preview only; never apply
	AllowDirty bool      // skip the clean-worktree precondition
	Verbose    bool      // echo the raw model response each iteration
	NoSchema   bool      // disable decode-time JSON-schema enforcement
	Budget     int       // token budget (prompt+completion); 0 = unlimited
	Out        io.Writer // progress sink
}

// RunDriver is the whole harness loop: a cheap model proposes a
// constrained plan, gorefactor applies it deterministically, the Go
// toolchain is the gate, and git is the rollback. The model never
// edits code and never sees line numbers -- it only fills a schema and
// reads structured failures.
//
// Unlike the agentic drivers, RunDriver keeps no growing message
// history (feedback is a single string overwritten each iteration), so
// Phase 1 masking does not apply here -- there is nothing to mask.
// Phase 2 (budget), Phase 4 (notes, read-only: single-shot has no tool
// call to write one), and Phase 6 (failure corpus) do apply and are
// wired in below.
func RunDriver(ctx context.Context, p Provider, cfg Config) (err error) {
	if cfg.Out == nil {
		cfg.Out = os.Stdout
	}
	if cfg.MaxIter <= 0 {
		cfg.MaxIter = 3
	}

	if !cfg.AllowDirty {
		if err := requireCleanWorktree(cfg.Dir); err != nil {
			return fmt.Errorf("require clean worktree: %w", err)
		}
	}

	// Operate from the target module so operation.File paths and the
	// build/test gate all resolve there.
	prev, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getwd: %w", err)
	}
	if err := os.Chdir(cfg.Dir); err != nil {
		return fmt.Errorf("chdir %s: %w", cfg.Dir, err)
	}
	defer os.Chdir(prev)

	notes := notesPromptSection(".")
	br := specBlastRadius(".", cfg.Spec)
	lastIter := 0
	defer func() { emitRunMetrics(cfg.Out, p, err, lastIter, br) }()

	feedback := ""
	for iter := 1; iter <= cfg.MaxIter; iter++ {
		lastIter = iter
		if done, perr := driverPuntOnBudget(cfg, p, iter); done {
			return perr
		}
		fmt.Fprintf(cfg.Out, "\n── iteration %d/%d ──\n", iter, cfg.MaxIter)

		plan, fb, perr := driverProposePlan(ctx, p, cfg, notes, feedback)
		if perr != nil {
			return perr
		}
		if fb != "" {
			feedback = fb
			continue
		}

		fb, done := driverApplyPlan(cfg, plan)
		if done {
			return nil
		}
		feedback = fb
	}

	return fmt.Errorf("no passing refactor after %d iteration(s); last failure:\n%s",
		cfg.MaxIter, feedback)

}

func driverPuntOnBudget(cfg Config, p Provider, iter int) (done bool, err error) {
	if cfg.Budget <= 0 {
		return false, nil
	}
	used := tokensUsed(p)
	if used < cfg.Budget {
		return false, nil
	}
	logFailure(".", failureEntry{Kind: failBudgetHit,
		Reason:  fmt.Sprintf("token budget %d exhausted (used %d)", cfg.Budget, used),
		Spec:    trim(cfg.Spec, 200),
		Context: fmt.Sprintf("iteration %d", iter)})
	return true, doPunt(cfg, "autopunt:budget_exhausted",
		fmt.Sprintf("token budget %d exhausted (used %d over %d iteration(s)); "+
			"stopping with a clean summary rather than spending past the accuracy plateau",
			cfg.Budget, used, iter-1), nil, iter)
}

func driverProposePlan(ctx context.Context, p Provider, cfg Config, notes, feedback string) (orchestrator.RefactoringPlan, string, error) {
	var plan orchestrator.RefactoringPlan
	var raw string
	var err error
	sys, usr := systemPrompt()+notes, buildUserPrompt(cfg.Spec, ".", feedback)
	if sc, ok := p.(schemaCompleter); ok && !cfg.NoSchema {
		raw, err = sc.CompleteSchema(ctx, sys, usr, planJSONSchema())
	} else {
		raw, err = p.Complete(ctx, sys, usr)
	}
	if err != nil {
		return plan, "", fmt.Errorf("provider call failed: %w", err)
	}
	if cfg.Verbose {
		fmt.Fprintf(cfg.Out, "  ┌ raw model response ──\n%s\n  └──\n", indent(trim(raw, 4000)))
	}

	js, err := extractPlanJSON(raw)
	if err != nil {

		fb := fmt.Sprintf("output was not valid JSON: %v", err)
		fmt.Fprintf(cfg.Out, "  ✗ %s\n  raw: %s\n", fb, trim(raw, 600))
		return plan, fb, nil
	}
	if js, err = normalizeToPlanJSON(js); err != nil {
		fb := fmt.Sprintf("could not normalize JSON to a plan: %v", err)
		fmt.Fprintf(cfg.Out, "  ✗ %s\n  raw: %s\n", fb, trim(raw, 600))
		return plan, fb, nil
	}
	if js, err = canonicalizePlanJSON(js); err != nil {
		fb := fmt.Sprintf("could not canonicalize plan: %v", err)
		fmt.Fprintf(cfg.Out, "  ✗ %s\n  raw: %s\n", fb, trim(raw, 600))
		return plan, fb, nil
	}

	if err := json.Unmarshal([]byte(js), &plan); err != nil {
		fb := fmt.Sprintf("plan JSON did not unmarshal: %v", err)
		fmt.Fprintf(cfg.Out, "  ✗ %s\n", fb)
		return plan, fb, nil
	}
	if plan.Version == "" {
		plan.Version = "1.0"
	}
	if plan.Name == "" {
		plan.Name = fmt.Sprintf("auto-%d", time.Now().UnixNano())
	}
	return plan, "", nil
}

func driverApplyPlan(cfg Config, plan orchestrator.RefactoringPlan) (feedback string, done bool) {
	o := orchestrator.NewOrchestrator()
	if err := o.RegisterPlan(&plan); err != nil {
		feedback = fmt.Sprintf("plan rejected by validator: %v", err)
		fmt.Fprintf(cfg.Out, "  ✗ %s\n", feedback)
		logFailure(".", failureEntry{Kind: failRejectedOp, Op: "plan:" + plan.Name,
			Reason: trim(feedback, 400), Spec: trim(cfg.Spec, 200)})
		return feedback, false
	}
	fmt.Fprintf(cfg.Out, "  plan %q: %d operation(s)\n", plan.Name, len(plan.Operations))

	dry, err := o.ExecutePlanDryRun(plan.Name)
	if err != nil {
		feedback = fmt.Sprintf("dry-run failed: %v", err)
		fmt.Fprintf(cfg.Out, "  ✗ %s\n", feedback)
		return feedback, false
	}
	fmt.Fprintf(cfg.Out, "%s\n", indent(strings.TrimSpace(dry.Summary)))

	if df := dryRunErrors(dry); df != "" {
		fmt.Fprintf(cfg.Out, "  ! dry-run warnings (non-blocking):\n%s\n", indent(strings.TrimSpace(df)))
	}

	if cfg.DryRun {
		fmt.Fprintln(cfg.Out, "  (dry-run: not applying)")
		return "", true
	}

	res, err := o.ExecutePlan(plan.Name)
	if err != nil || !res.Success {
		feedback = "apply failed:\n" + execErrors(res, err)
		fmt.Fprintf(cfg.Out, "  ✗ %s\n", feedback)
		logFailure(".", failureEntry{Kind: failRejectedOp, Op: "plan:" + plan.Name,
			Reason: trim(feedback, 400), Spec: trim(cfg.Spec, 200)})
		rollback(cfg.Dir, cfg.Out)
		return feedback, false
	}

	ok, out := runGate(".")
	if ok {
		fmt.Fprintf(cfg.Out, "  ✓ gate passed (go build + go test); changes applied\n")
		return "", true
	}
	fmt.Fprintf(cfg.Out, "  ✗ gate failed; rolling back\n%s\n", indent(out))
	logFailure(".", failureEntry{Kind: failRejectedOp, Op: "plan:" + plan.Name,
		Reason: trim("gate failed:\n"+out, 400), Spec: trim(cfg.Spec, 200)})
	rollback(cfg.Dir, cfg.Out)
	return "the refactor broke the build/test gate:\n" + out, false
}

// requireCleanWorktree enforces total, safe rollback: if the tree is
// clean, `reset --hard` + `clean -fd` perfectly undoes any attempt.
func requireCleanWorktree(dir string) error {
	if out, err := exec.Command("git", "-C", dir, "rev-parse", "--is-inside-work-tree").CombinedOutput(); err != nil {
		return fmt.Errorf("target %s is not a git work tree (needed for safe rollback): %s",
			dir, strings.TrimSpace(string(out)))
	}
	out, err := exec.Command("git", "-C", dir, "status", "--porcelain").CombinedOutput()
	if err != nil {
		return fmt.Errorf("git status: %w", err)
	}
	if strings.TrimSpace(string(out)) != "" {
		return fmt.Errorf("working tree is dirty; commit/stash first or pass -allow-dirty")
	}
	return nil
}

func rollback(dir string, out io.Writer) {
	if err := exec.Command("git", "-C", dir, "reset", "--hard").Run(); err != nil {
		fmt.Fprintf(out, "  ! rollback reset failed: %v\n", err)
	}
	if err := exec.Command("git", "-C", dir, "clean", "-fd").Run(); err != nil {
		fmt.Fprintf(out, "  ! rollback clean failed: %v\n", err)
	}
}

// runGate is doctor's behavioural core: build then test. A green gate
// is the only thing that lets a refactor stand.
func runGate(dir string) (bool, string) {
	if out, err := runIn(dir, "go", "build", "./..."); err != nil {
		return false, "go build ./...\n" + out
	}
	if out, err := runIn(dir, "go", "test", "./..."); err != nil {
		return false, "go test ./...\n" + out
	}
	// Third leg of the gate (design plan): no new error-severity doctor
	// findings vs HEAD. Advisory mode reports them in the success output
	// instead of blocking; hard mode fails the gate.
	blocking, advisory := runDoctorGate(dir, false)
	if blocking != "" {
		return false, "doctor gate (new findings vs HEAD):\n" + blocking
	}
	return true, advisory

}

// runInWithStdin runs a command with the given string piped to stdin.
func runInWithStdin(dir, stdin, name string, args ...string) (string, error) {
	c := exec.Command(name, args...)
	c.Dir = dir
	env := append(analyzer.SanitizedGitEnv(), "GOTOOLCHAIN=auto")

	if v := os.Getenv("GOTMPDIR"); v != "" {
		env = append(env, "GOTMPDIR="+v)
	}
	if v := os.Getenv("GOCACHE"); v != "" {
		env = append(env, "GOCACHE="+v)
	}
	c.Env = env
	c.Stdin = strings.NewReader(stdin)
	b, err := c.CombinedOutput()
	return trim(string(b), 1500), err
}

func runIn(dir, name string, args ...string) (string, error) {
	c := exec.Command(name, args...)
	c.Dir = dir
	env := append(analyzer.SanitizedGitEnv(), "GOTOOLCHAIN=auto")

	// If /tmp is noexec (common in some container environments), the caller can
	// set GOTMPDIR to a directory that allows execution. We honour it here so
	// go test can write and execute test binaries. GOCACHE follows suit.
	if v := os.Getenv("GOTMPDIR"); v != "" {
		env = append(env, "GOTMPDIR="+v)
	}
	if v := os.Getenv("GOCACHE"); v != "" {
		env = append(env, "GOCACHE="+v)
	}
	c.Env = env
	b, err := c.CombinedOutput()
	return trim(string(b), 1500), err
}

func dryRunErrors(d *orchestrator.DryRunResult) string {
	var b strings.Builder
	for _, op := range d.Operations {
		if !op.Success {
			fmt.Fprintf(&b, "- %s: %s\n", op.Operation.Type, op.Error)
		}
	}
	return b.String()
}

func execErrors(res *orchestrator.ExecutionResult, err error) string {
	var b strings.Builder
	if err != nil {
		fmt.Fprintf(&b, "%v\n", err)
	}
	if res != nil {
		for _, e := range res.Errors {
			fmt.Fprintf(&b, "- %s\n", e)
		}
	}
	return b.String()
}

func indent(s string) string {
	if s == "" {
		return ""
	}
	return "  " + strings.ReplaceAll(s, "\n", "\n  ")
}

func trim(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	// Head+tail: preserve first error context (head) and final state (tail).
	head := max * 2 / 3
	tail := max - head - 30
	if tail < 60 {
		return s[:max] + "\n…(truncated)"
	}
	omitted := len(s) - head - tail
	return s[:head] + fmt.Sprintf("\n…(%d bytes omitted)…\n", omitted) + s[len(s)-tail:]
}
