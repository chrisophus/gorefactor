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
	Out        io.Writer // progress sink
}

// RunDriver is the whole harness loop: a cheap model proposes a
// constrained plan, gorefactor applies it deterministically, the Go
// toolchain is the gate, and git is the rollback. The model never
// edits code and never sees line numbers -- it only fills a schema and
// reads structured failures.
func RunDriver(ctx context.Context, p Provider, cfg Config) error {
	if cfg.Out == nil {
		cfg.Out = os.Stdout
	}
	if cfg.MaxIter <= 0 {
		cfg.MaxIter = 3
	}

	if !cfg.AllowDirty {
		if err := requireCleanWorktree(cfg.Dir); err != nil {
			return err
		}
	}

	// Operate from the target module so operation.File paths and the
	// build/test gate all resolve there.
	prev, err := os.Getwd()
	if err != nil {
		return err
	}
	if err := os.Chdir(cfg.Dir); err != nil {
		return fmt.Errorf("chdir %s: %w", cfg.Dir, err)
	}
	defer os.Chdir(prev)

	feedback := ""
	for iter := 1; iter <= cfg.MaxIter; iter++ {
		fmt.Fprintf(cfg.Out, "\n── iteration %d/%d ──\n", iter, cfg.MaxIter)

		var raw string
		sys, usr := systemPrompt(), buildUserPrompt(cfg.Spec, ".", feedback)
		if sc, ok := p.(schemaCompleter); ok && !cfg.NoSchema {
			raw, err = sc.CompleteSchema(ctx, sys, usr, planJSONSchema())
		} else {
			raw, err = p.Complete(ctx, sys, usr)
		}
		if err != nil {
			return fmt.Errorf("provider call failed: %w", err)
		}
		if cfg.Verbose {
			fmt.Fprintf(cfg.Out, "  ┌ raw model response ──\n%s\n  └──\n", indent(trim(raw, 4000)))
		}

		js, err := extractPlanJSON(raw)
		if err != nil {
			// Echo what the model actually returned -- without it the
			// first live runs of a cheap model are undebuggable.
			feedback = fmt.Sprintf("output was not valid JSON: %v", err)
			fmt.Fprintf(cfg.Out, "  ✗ %s\n  raw: %s\n", feedback, trim(raw, 600))
			continue
		}
		if js, err = normalizeToPlanJSON(js); err != nil {
			feedback = fmt.Sprintf("could not normalize JSON to a plan: %v", err)
			fmt.Fprintf(cfg.Out, "  ✗ %s\n  raw: %s\n", feedback, trim(raw, 600))
			continue
		}
		if js, err = canonicalizePlanJSON(js); err != nil {
			feedback = fmt.Sprintf("could not canonicalize plan: %v", err)
			fmt.Fprintf(cfg.Out, "  ✗ %s\n  raw: %s\n", feedback, trim(raw, 600))
			continue
		}

		var plan orchestrator.RefactoringPlan
		if err := json.Unmarshal([]byte(js), &plan); err != nil {
			feedback = fmt.Sprintf("plan JSON did not unmarshal: %v", err)
			fmt.Fprintf(cfg.Out, "  ✗ %s\n", feedback)
			continue
		}
		if plan.Version == "" {
			plan.Version = "1.0"
		}
		if plan.Name == "" {
			plan.Name = fmt.Sprintf("auto-%d", time.Now().UnixNano())
		}

		o := orchestrator.NewOrchestrator()
		if err := o.RegisterPlan(&plan); err != nil {
			feedback = fmt.Sprintf("plan rejected by validator: %v", err)
			fmt.Fprintf(cfg.Out, "  ✗ %s\n", feedback)
			continue
		}
		fmt.Fprintf(cfg.Out, "  plan %q: %d operation(s)\n", plan.Name, len(plan.Operations))

		dry, err := o.ExecutePlanDryRun(plan.Name)
		if err != nil {
			feedback = fmt.Sprintf("dry-run failed: %v", err)
			fmt.Fprintf(cfg.Out, "  ✗ %s\n", feedback)
			continue
		}
		fmt.Fprintf(cfg.Out, "%s\n", indent(strings.TrimSpace(dry.Summary)))
		// Dry-run is an advisory preview only. gorefactor's simulation
		// is incomplete for some ops (e.g. create_file reads a file
		// that does not exist yet), so a dry-run problem is NOT
		// blocking. The authoritative sensors are ExecutePlan success
		// and the build+test gate; safe git rollback backstops apply.
		if df := dryRunErrors(dry); df != "" {
			fmt.Fprintf(cfg.Out, "  ! dry-run warnings (non-blocking):\n%s\n", indent(strings.TrimSpace(df)))
		}

		if cfg.DryRun {
			fmt.Fprintln(cfg.Out, "  (dry-run: not applying)")
			return nil
		}

		res, err := o.ExecutePlan(plan.Name)
		if err != nil || !res.Success {
			feedback = "apply failed:\n" + execErrors(res, err)
			fmt.Fprintf(cfg.Out, "  ✗ %s\n", feedback)
			rollback(cfg.Dir, cfg.Out)
			continue
		}

		ok, out := runGate(".")
		if ok {
			fmt.Fprintf(cfg.Out, "  ✓ gate passed (go build + go test); changes applied\n")
			return nil
		}
		fmt.Fprintf(cfg.Out, "  ✗ gate failed; rolling back\n%s\n", indent(out))
		rollback(cfg.Dir, cfg.Out)
		feedback = "the refactor broke the build/test gate:\n" + out
	}

	return fmt.Errorf("no passing refactor after %d iteration(s); last failure:\n%s",
		cfg.MaxIter, feedback)
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
	return true, ""
}

func runIn(dir, name string, args ...string) (string, error) {
	c := exec.Command(name, args...)
	c.Dir = dir
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
	if len(s) > max {
		return s[:max] + "\n…(truncated)"
	}
	return s
}
