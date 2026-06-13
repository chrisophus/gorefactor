package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/chrisophus/gorefactor/orchestrator"
)

// mutationResult is the shared --json result shape for mutation commands.
type mutationResult struct {
	Success      bool     `json:"success"`
	Operation    string   `json:"operation"`
	File         string   `json:"file,omitempty"`
	Detail       string   `json:"detail,omitempty"`
	FilesChanged []string `json:"filesChanged,omitempty"`
	LinesChanged int      `json:"linesChanged"`
	UndoToken    string   `json:"undoToken,omitempty"`
	DryRun       bool     `json:"dryRun,omitempty"`
	Diff         string   `json:"diff,omitempty"`
	Error        string   `json:"error,omitempty"`
	Candidates   []string `json:"candidates,omitempty"`
}

func emitJSON(v interface{}) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

// mutation runs a mutating command with universal snapshot/journal support,
// --dry-run diffs, --json output, and the --gate build check.
type mutation struct {
	op      string   // operation name for the journal and JSON output
	file    string   // primary file
	files   []string // all files the operation may touch (defaults to [file])
	jsonOut bool
	dryRun  bool
	gate    bool
	quiet   bool // suppress the human success line (command prints its own)
}

// mutFlagSpec is the flag set shared by all mutation commands.
func mutFlagSpec(extra map[string]bool) map[string]bool {
	spec := map[string]bool{"--json": false, "--dry-run": false, "--gate": false}
	for k, v := range extra {
		spec[k] = v
	}
	return spec
}

func (m *mutation) setCommonFlags(flags map[string]string) {
	m.jsonOut = flags["--json"] != ""
	m.dryRun = flags["--dry-run"] != ""
	m.gate = flags["--gate"] != ""
}

// fail renders an error for --json consumers and returns it unchanged so
// main can map it to a semantic exit code.
func (m *mutation) fail(err error) error {
	if m.jsonOut {
		emitJSON(mutationResult{
			Success:    false,
			Operation:  m.op,
			File:       m.file,
			Error:      err.Error(),
			Candidates: errCandidates(err),
		})
	}
	return err
}

// run executes apply with snapshot/journal/dry-run/gate handling.
// apply returns the human-readable success detail.
func (m *mutation) run(apply func() (string, error)) error {
	files := m.files
	if len(files) == 0 && m.file != "" {
		files = []string{m.file}
	}
	before := map[string][]byte{}
	for _, f := range files {
		if b, err := os.ReadFile(f); err == nil {
			before[f] = b
		}
	}

	detail, err := apply()
	if err != nil {
		m.restore(before, files)
		return m.fail(err)
	}

	var changed, created []string
	linesChanged := 0
	var diffBuf strings.Builder
	needDiff := m.dryRun || m.jsonOut
	for _, f := range files {
		after, rerr := os.ReadFile(f)
		b, existed := before[f]
		switch {
		case rerr != nil && !existed:
			continue // never existed, still doesn't
		case rerr != nil && existed:
			changed = append(changed, f)
			if needDiff {
				linesChanged += diffLineCount(string(b), "")
				diffBuf.WriteString(unifiedDiff(f, string(b), ""))
			}
		case !existed:
			changed = append(changed, f)
			created = append(created, f)
			if needDiff {
				linesChanged += diffLineCount("", string(after))
				diffBuf.WriteString(unifiedDiff(f, "", string(after)))
			}
		case !bytes.Equal(b, after):
			changed = append(changed, f)
			if needDiff {
				linesChanged += diffLineCount(string(b), string(after))
				diffBuf.WriteString(unifiedDiff(f, string(b), string(after)))
			}
		}
	}

	if m.dryRun {
		m.restore(before, files)
		if m.jsonOut {
			emitJSON(mutationResult{
				Success:      true,
				Operation:    m.op,
				File:         m.file,
				Detail:       detail,
				FilesChanged: changed,
				LinesChanged: linesChanged,
				DryRun:       true,
				Diff:         diffBuf.String(),
			})
		} else {
			fmt.Printf("[dry-run] %s — no files written\n", detail)
			fmt.Print(diffBuf.String())
		}
		return nil
	}

	undoToken := ""
	if len(changed) > 0 {
		beforeChanged := map[string][]byte{}
		var createdOnly []string
		for _, f := range changed {
			if b, ok := before[f]; ok {
				beforeChanged[f] = b
			} else {
				createdOnly = append(createdOnly, f)
			}
		}
		if activeTxn != nil {
			// Inside a transaction the txn command journals once for the
			// whole batch; individual operations only feed the collector.
			activeTxn.record(beforeChanged, createdOnly)
		} else {
			entry, jerr := orchestrator.RecordOperation(m.op, detail, beforeChanged, createdOnly)
			if jerr != nil {
				fmt.Fprintf(os.Stderr, "warning: journal write failed: %v\n", jerr)
			} else {
				undoToken = entry.ID
			}
		}
	}

	if m.gate && len(changed) > 0 {
		if gerr := buildAffectedPackages(changed); gerr != nil {
			m.restore(before, files)
			if undoToken != "" {
				_ = orchestrator.DropJournalEntry(undoToken)
			}
			return m.fail(gateErrorf("gate: build failed after %s; changes rolled back\n%v", m.op, gerr))
		}
	}

	if m.jsonOut {
		emitJSON(mutationResult{
			Success:      true,
			Operation:    m.op,
			File:         m.file,
			Detail:       detail,
			FilesChanged: changed,
			LinesChanged: linesChanged,
			UndoToken:    undoToken,
		})
	} else if !m.quiet {
		fmt.Println(detail)
	}
	return nil
}

// restore puts files back to their captured pre-mutation state, removing
// files that did not exist before.
func (m *mutation) restore(before map[string][]byte, files []string) {
	for _, f := range files {
		if b, ok := before[f]; ok {
			_ = os.WriteFile(f, b, 0644)
		} else if _, err := os.Stat(f); err == nil {
			_ = os.Remove(f)
		}
	}
}

// buildAffectedPackages runs `go build` in each package directory containing
// a changed .go file.
func buildAffectedPackages(changed []string) error {
	dirs := map[string]bool{}
	for _, f := range changed {
		if strings.HasSuffix(f, ".go") {
			dirs[filepath.Dir(f)] = true
		}
	}
	sorted := make([]string, 0, len(dirs))
	for d := range dirs {
		sorted = append(sorted, d)
	}
	sort.Strings(sorted)
	for _, dir := range sorted {
		cmd := exec.Command("go", "build", ".")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("go build %s:\n%s", dir, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

// packageGoFiles lists the .go files sharing a directory with file
// (package-wide mutations like rename touch all of them).
func packageGoFiles(file string) []string {
	dir := filepath.Dir(file)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []string{file}
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	if len(files) == 0 {
		return []string{file}
	}
	return files
}

// runPlanOps registers and executes a one-off plan with the orchestrator's
// own snapshotting disabled (the mutation runner journals instead) and
// converts a failed result into an error.
func runPlanOps(name string, ops []*orchestrator.RefactoringOperation) error {
	plan := &orchestrator.RefactoringPlan{
		Version:    "1.0",
		Name:       name,
		Operations: ops,
	}
	orch := orchestrator.NewOrchestrator()
	orch.SkipSnapshot = true
	if err := orch.RegisterPlan(plan); err != nil {
		return err
	}
	result, err := orch.ExecutePlan(plan.Name)
	if err != nil {
		return err
	}
	if !result.Success {
		return fmt.Errorf("%s", strings.Join(result.Errors, "; "))
	}
	return nil
}
