package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// agent_corpus.go: the Slice-2 runner. For each task it materializes the
// fixture as a throwaway git repo, invokes gorefactor-agent against it, parses
// the emitted blocks, and tallies predicted-vs-actual outcome. -run actually
// calls the LLM (costs tokens); without it the runner only lists the corpus.

type corpusOpts struct {
	run        bool
	only       string
	difficulty string
	provider   string
	model      string
	budget     int
	agentBin   string
	verbose    bool
}

// materializeFixture writes every fixture file under dir.
func materializeFixture(dir string, files map[string]string) error {
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// gitInitCommit turns dir into a clean git repo (the agent's clean-worktree
// precondition), with an isolated identity so it works on any machine.
func gitInitCommit(dir string) error {
	steps := [][]string{
		{"init", "-q"},
		{"add", "-A"},
		{"-c", "user.email=corpus@local", "-c", "user.name=corpus", "commit", "-qm", "fixture"},
	}
	for _, args := range steps {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, out)
		}
	}
	return nil
}

// taskResult is the observed outcome of one corpus run: the self-reported
// outcome plus the verdict of the structural intent-oracle. A run only "passes"
// when the outcome matches the prediction AND every oracle check holds — a green
// build/test outcome with a failing oracle means the agent stayed green without
// doing the intended transform.
type taskResult struct {
	stdout     string
	actual     expectedOutcome
	oraclePass bool
	oracleFail []string
}

// runAgentTask executes one task against a fresh fixture and returns its
// observed outcome plus the intent-oracle verdict.
func runAgentTask(o corpusOpts, t agentTask) (taskResult, error) {
	dir, err := os.MkdirTemp("", "corpus-"+t.ID+"-")
	if err != nil {
		return taskResult{actual: outError}, err
	}
	defer os.RemoveAll(dir)
	if err := materializeFixture(dir, t.Fixture); err != nil {
		return taskResult{actual: outError}, err
	}
	if err := gitInitCommit(dir); err != nil {
		return taskResult{actual: outError}, err
	}

	args := []string{"-spec", t.Spec, "-provider", o.provider, "-model", o.model, "-dir", dir}
	if o.budget > 0 {
		args = append(args, "-budget", fmt.Sprintf("%d", o.budget))
	}
	cmd := exec.Command(o.agentBin, args...)
	out, _ := cmd.CombinedOutput() // exit code is carried in RUN_METRICS/blocks

	// Evaluate the intent-oracle against the mutated fixture BEFORE the deferred
	// cleanup removes it. No asserts declared → vacuously passes.
	oraclePass, oracleFail := evalOracle(dir, t.Assert)
	return taskResult{
		stdout:     string(out),
		actual:     classifyOutcome(string(out)),
		oraclePass: oraclePass,
		oracleFail: oracleFail,
	}, nil
}

// runAgentCorpus lists (or with -run, executes) the corpus and prints a table
// of predicted-vs-actual outcomes plus a pass/fail tally.
func runAgentCorpus(root string, o corpusOpts) {
	if o.agentBin == "" {
		o.agentBin = filepath.Join(root, "gorefactor-agent")
	}
	if o.run {
		if _, err := os.Stat(o.agentBin); err != nil {
			fmt.Fprintln(os.Stderr, "build gorefactor-agent first: go build -o gorefactor-agent ./cmd/gorefactor-agent")
			os.Exit(1)
		}
	}

	tasks := selectTasks(agentTasks(), o.only, o.difficulty)
	fmt.Printf("%-18s  %-8s  %-30s  %-9s  %-9s  %s\n",
		"id", "level", "probes", "expected", "actual", "match")
	fmt.Println(strings.Repeat("-", 96))

	var runCount, matchCount int
	for _, t := range tasks {
		actual := expectedOutcome("(not run)")
		match := ""
		if o.run {
			res, err := runAgentTask(o, t)
			got := res.actual
			if err != nil {
				got = outError
			}
			actual = got
			runCount++
			switch {
			case got != t.Expected:
				match = "DIFF"
			case !res.oraclePass:
				// Outcome matched but the transform didn't provably happen.
				match = "ORACLE-FAIL"
			default:
				matchCount++
				match = "OK"
			}
			if o.verbose {
				if rm, ok := parseRunMetrics(res.stdout); ok {
					fmt.Printf("    ↳ %s: %d steps, %d tokens\n", t.ID, rm.Steps, rm.LocalTokens)
				}
			}
			for _, f := range res.oracleFail {
				fmt.Printf("    ✗ oracle: %s\n", f)
			}
		}
		fmt.Printf("%-18s  %-8s  %-30s  %-9s  %-9s  %s\n",
			t.ID, t.Difficulty, t.Probes, t.Expected, actual, match)
	}
	fmt.Println(strings.Repeat("-", 96))
	if o.run {
		fmt.Printf("%d/%d tasks matched their predicted outcome\n", matchCount, runCount)
	} else {
		fmt.Printf("%d tasks listed (pass -agent-corpus-run to execute against the junior)\n", len(tasks))
	}
}

// selectTasks filters the corpus by id and/or difficulty (empty = all).
func selectTasks(all []agentTask, only, difficulty string) []agentTask {
	var out []agentTask
	for _, t := range all {
		if only != "" && t.ID != only {
			continue
		}
		if difficulty != "" && t.Difficulty != difficulty {
			continue
		}
		out = append(out, t)
	}
	return out
}
