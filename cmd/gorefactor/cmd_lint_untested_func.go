package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type untestedFunctionRule struct{}

func (untestedFunctionRule) Name() string { return "untested-function" }

func (r untestedFunctionRule) Run(ctx LintContext) []lintIssue {
	coverPath := filepath.Join(ctx.Root, "coverage.out")
	if _, err := os.Stat(coverPath); err != nil {
		return nil
	}
	cmd := exec.Command("go", "tool", "cover", "-func", coverPath)
	cmd.Dir = ctx.Root
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var findings []lintIssue
	for _, line := range strings.Split(string(out), "\n") {
		if line == "" || strings.HasPrefix(line, "total:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		pct := fields[len(fields)-1]
		if pct != "0.0%" {
			continue
		}
		fnName, ok := exportedFuncName(fields[len(fields)-2])
		if !ok {
			continue
		}
		if strings.HasPrefix(fnName, "Test") || strings.HasPrefix(fnName, "Example") || strings.HasPrefix(fnName, "Benchmark") {
			continue
		}
		loc := strings.TrimSuffix(fields[0], ":")
		findings = append(findings, lintIssue{
			File:     loc,
			Rule:     "untested-function",
			Severity: "info",
			Message:  fmt.Sprintf("exported %s has 0%% coverage", fnName),
		})
	}
	return findings
}

// exportedFuncName extracts the symbol name from a `go tool cover -func`
// entry (handles "Func", "(Recv).Method", "(*Recv).Method") and returns
// (name, true) only if it's exported.
func exportedFuncName(raw string) (string, bool) {
	name := raw
	if idx := strings.LastIndex(name, ")."); idx >= 0 {
		name = name[idx+2:]
	} else if idx := strings.LastIndex(name, "."); idx >= 0 {
		name = name[idx+1:]
	}
	if len(name) == 0 {
		return "", false
	}
	c := name[0]
	return name, c >= 'A' && c <= 'Z'
}
