package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
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
	models     string // comma-separated model sweep (overrides model when set)
	modes      string // comma-separated harness-mode sweep (agentic only; kept for sweep cells)
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
	metrics    runMetrics // parsed RUN_METRICS (zero if the block was absent)
	wallMs     int64      // wall-clock of the agent invocation
}

// runAgentTask executes one task against a fresh fixture under the given
// sweep cell (provider/model/mode) and returns its observed outcome, the
// intent-oracle verdict, and its parsed cost metrics.
func runAgentTask(o corpusOpts, cell sweepCell, t agentTask) (taskResult, error) {
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

	args := []string{"-spec", t.Spec, "-provider", cell.provider, "-model", cell.model, "-dir", dir}
	if o.budget > 0 {
		args = append(args, "-budget", fmt.Sprintf("%d", o.budget))
	}
	cmd := exec.Command(o.agentBin, args...)
	start := time.Now()
	out, _ := cmd.CombinedOutput() // exit code is carried in RUN_METRICS/blocks
	wallMs := time.Since(start).Milliseconds()

	// Evaluate the intent-oracle against the mutated fixture BEFORE the deferred
	// cleanup removes it. No asserts declared → vacuously passes.
	oraclePass, oracleFail := evalOracle(dir, t.Assert)
	rm, _ := parseRunMetrics(string(out))
	return taskResult{
		stdout:     string(out),
		actual:     classifyOutcome(string(out)),
		oraclePass: oraclePass,
		oracleFail: oracleFail,
		metrics:    rm,
		wallMs:     wallMs,
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

	// Dry list (no -run): a single table of predictions, no sweep.
	if !o.run {
		fmt.Printf("%-18s  %-8s  %-30s  %-9s\n", "id", "level", "probes", "expected")
		fmt.Println(strings.Repeat("-", 70))
		for _, t := range tasks {
			fmt.Printf("%-18s  %-8s  %-30s  %-9s\n", t.ID, t.Difficulty, t.Probes, t.Expected)
		}
		fmt.Println(strings.Repeat("-", 70))
		fmt.Printf("%d tasks listed (pass -agent-corpus-run to execute against the junior)\n", len(tasks))
		return
	}

	cells := buildCells(o)
	summaries := make([]cellSummary, 0, len(cells))
	for _, cell := range cells {
		summaries = append(summaries, runCell(o, cell, tasks))
	}

	// A model×mode sweep (more than one cell) gets a cost-of-pass matrix.
	if len(cells) > 1 {
		printSweepMatrix(summaries)
	}
}

// runCell executes every task under one sweep cell, prints the per-task
// table, and returns the aggregated cost summary.
func runCell(o corpusOpts, cell sweepCell, tasks []agentTask) cellSummary {
	fmt.Printf("\n=== %s / %s / %s ===\n", cell.provider, cell.model, cell.mode)
	fmt.Printf("%-18s  %-8s  %-30s  %-9s  %-9s  %s\n",
		"id", "level", "probes", "expected", "actual", "match")
	fmt.Println(strings.Repeat("-", 96))

	sum := cellSummary{provider: cell.provider, model: cell.model, mode: cell.mode}
	for _, t := range tasks {
		res, err := runAgentTask(o, cell, t)
		got := res.actual
		if err != nil {
			got = outError
		}
		pass := got == t.Expected && res.oraclePass
		match := "OK"
		switch {
		case got != t.Expected:
			match = "DIFF"
		case !res.oraclePass:
			match = "ORACLE-FAIL"
		}
		sum.observe(cell.model, pass, res)
		if o.verbose {
			fmt.Printf("    ↳ %s: %d steps, %d/%d in/out tokens, %dms\n",
				t.ID, res.metrics.Steps, res.metrics.PromptTokens, res.metrics.CompletionTokens, res.wallMs)
		}
		for _, f := range res.oracleFail {
			fmt.Printf("    ✗ oracle: %s\n", f)
		}
		fmt.Printf("%-18s  %-8s  %-30s  %-9s  %-9s  %s\n",
			t.ID, t.Difficulty, t.Probes, t.Expected, got, match)
	}
	fmt.Println(strings.Repeat("-", 96))
	printCellSummary(sum)
	return sum
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
