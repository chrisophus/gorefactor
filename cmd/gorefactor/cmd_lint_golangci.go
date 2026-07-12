package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// golangciLintRule wraps `golangci-lint run` output as lint issues. It is deliberately NOT part of
// defaultLintRules(): `gorefactor lint` stays a fast in-process structural sensor, while this
// subprocess-backed rule runs as its own stage in `gorefactor doctor` (the aggregate final gate).
type golangciLintRule struct{}

func (golangciLintRule) Name() string { return "golangci-lint" }

func (r golangciLintRule) Run(ctx LintContext) []lintIssue {
	if !golangciLintAvailable(ctx.Root) {
		return nil
	}
	cmd := exec.Command("golangci-lint", "run",
		"--output.json.path", "stdout",
		"--output.text.path", "/dev/null",
		"./...",
	)
	cmd.Dir = ctx.Root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, runErr := cmd.Output()
	if len(out) == 0 {
		if runErr != nil {
			msg := strings.TrimSpace(stderr.String())
			if msg == "" {
				msg = runErr.Error()
			}
			return []lintIssue{{
				Rule:     "golangci-lint",
				Severity: "error",
				Message:  fmt.Sprintf("golangci-lint failed to run: %s", msg),
			}}
		}
		return nil
	}
	var report struct {
		Issues []struct {
			FromLinter string `json:"FromLinter"`
			Text       string `json:"Text"`
			Severity   string `json:"Severity"`
			Pos        struct {
				Filename string `json:"Filename"`
				Line     int    `json:"Line"`
			} `json:"Pos"`
		} `json:"Issues"`
	}
	if err := json.NewDecoder(strings.NewReader(string(out))).Decode(&report); err != nil {
		return nil
	}
	var issues []lintIssue
	for _, iss := range report.Issues {
		sev := iss.Severity
		if sev == "" {
			sev = "warning"
		}
		issues = append(issues, lintIssue{
			File:     iss.Pos.Filename,
			Rule:     "golangci-lint",
			Severity: sev,
			Message:  fmt.Sprintf("[%s] %s (line %d)", iss.FromLinter, iss.Text, iss.Pos.Line),
		})
	}
	return issues

}

func golangciLintAvailable(root string) bool {
	if _, err := exec.LookPath("golangci-lint"); err != nil {
		return false
	}
	for _, name := range []string{".golangci.yml", ".golangci.yaml", ".golangci.toml", ".golangci.json"} {
		if _, err := os.Stat(filepath.Join(root, name)); err == nil {
			return true
		}
	}
	return false
}
