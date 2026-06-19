package main

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/chrisophus/gorefactor/analyzer"
)

func init() {
	registerCommand(Command{
		Name:        "review",
		Description: "Structural quality review of changed functions vs a git ref [--json]",
		Usage:       "review [git-ref] [--json]",
		MinArgs:     0,
		MaxArgs:     1,
		Flags:       map[string]bool{"--json": false},
		Run:         reviewCommand,
	})
}

// reviewFinding is one per-function quality observation.
type reviewFinding struct {
	File     string `json:"file"`
	Function string `json:"function"`
	Line     int    `json:"line"`
	Severity string `json:"severity"`
	Rule     string `json:"rule"`
	Message  string `json:"message"`
}

type reviewResult struct {
	Ref      string          `json:"ref"`
	Findings []reviewFinding `json:"findings"`
}

// thresholds
const (
	reviewLongFunctionLines   = 75
	reviewLineGrowthThreshold = 20
	reviewNestingThreshold    = 5
	reviewComplexityGrowthMin = 5
)

func printReview(res *reviewResult) {
	if len(res.Findings) == 0 {
		fmt.Printf("review vs %s: no findings\n", res.Ref)
		return
	}
	fmt.Printf("review vs %s: %d finding(s)\n", res.Ref, len(res.Findings))
	for _, f := range res.Findings {
		fmt.Printf("%s:%d [%s] %s: %s\n", f.File, f.Line, f.Severity, f.Rule, f.Message)
	}
}

func computeReview(ref string) (*reviewResult, error) {
	prefix, err := reviewGitShowPrefix()
	if err != nil {
		return nil, fmt.Errorf("review requires a git repository: %w", err)
	}

	changedFiles, err := reviewChangedGoFiles(ref, prefix)
	if err != nil {
		return nil, err
	}

	res := &reviewResult{Ref: ref, Findings: []reviewFinding{}}
	for _, file := range changedFiles {
		findings, ferr := reviewFile(ref, prefix, file)
		if ferr != nil {
			continue
		}
		res.Findings = append(res.Findings, findings...)
	}

	sort.Slice(res.Findings, func(i, j int) bool {
		a, b := res.Findings[i], res.Findings[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Rule < b.Rule
	})
	return res, nil
}

// reviewGitShowPrefix returns the path prefix of the current directory relative
// to the git repo root, e.g. "cmd/gorefactor/" or "".
func reviewGitShowPrefix() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--show-prefix").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// reviewChangedGoFiles returns .go files changed vs ref, relative to cwd.
func reviewChangedGoFiles(ref, prefix string) ([]string, error) {
	out, err := exec.Command("git", "diff", "--name-only", ref).Output()
	if err != nil {
		return nil, fmt.Errorf("git diff --name-only %s failed: %w", ref, err)
	}
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasSuffix(line, ".go") {
			continue
		}
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		rel := strings.TrimPrefix(line, prefix)
		files = append(files, rel)
	}
	return files, nil
}

// reviewFileAt returns the content of a file at a git ref.
func reviewFileAt(ref, fullPath string) ([]byte, error) {
	return exec.Command("git", "show", ref+":"+filepath.ToSlash(fullPath)).Output()
}

// reviewFile compares per-function metrics for one file between ref and disk.
func reviewFile(ref, prefix, relFile string) ([]reviewFinding, error) {
	newMetrics, err := analyzer.FunctionMetricsForFile(relFile)
	if err != nil {
		return nil, err
	}

	// Build a map of old metrics (best-effort; empty for new files).
	oldMetrics := map[string]analyzer.FunctionMetrics{}
	oldSrc, gerr := reviewFileAt(ref, prefix+relFile)
	if gerr == nil {
		oldList, perr := analyzer.FunctionMetricsForSource(relFile, oldSrc)
		if perr == nil {
			for _, m := range oldList {
				oldMetrics[m.Key()] = m
			}
		}
	}

	var out []reviewFinding
	for _, m := range newMetrics {
		key := m.Key()
		old, existed := oldMetrics[key]

		// Rule: absolute long-function threshold.
		if m.Lines >= reviewLongFunctionLines {
			sev := "warning"
			if m.Lines >= reviewLongFunctionLines*2 {
				sev = "error"
			}
			out = append(out, reviewFinding{
				File:     relFile,
				Function: key,
				Line:     m.Line,
				Severity: sev,
				Rule:     "long-function",
				Message:  fmt.Sprintf("%s is %d lines (threshold %d)", key, m.Lines, reviewLongFunctionLines),
			})
		}

		// Rule: line count growth since ref.
		if existed && m.Lines-old.Lines >= reviewLineGrowthThreshold {
			out = append(out, reviewFinding{
				File:     relFile,
				Function: key,
				Line:     m.Line,
				Severity: "warning",
				Rule:     "line-growth",
				Message:  fmt.Sprintf("%s grew by %d lines (%d → %d)", key, m.Lines-old.Lines, old.Lines, m.Lines),
			})
		}

		// Rule: deep nesting absolute.
		if m.MaxNesting > reviewNestingThreshold {
			out = append(out, reviewFinding{
				File:     relFile,
				Function: key,
				Line:     m.Line,
				Severity: "warning",
				Rule:     "deep-nesting",
				Message:  fmt.Sprintf("%s has nesting depth %d (threshold %d)", key, m.MaxNesting, reviewNestingThreshold),
			})
		}

		// Rule: complexity increase since ref.
		if existed && m.Complexity-old.Complexity >= reviewComplexityGrowthMin {
			out = append(out, reviewFinding{
				File:     relFile,
				Function: key,
				Line:     m.Line,
				Severity: "warning",
				Rule:     "complexity-increase",
				Message:  fmt.Sprintf("%s complexity increased by %d (%d → %d)", key, m.Complexity-old.Complexity, old.Complexity, m.Complexity),
			})
		}
	}
	return out, nil
}
