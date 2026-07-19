package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

type archLintRule struct{}

func (r archLintRule) Run(ctx LintContext) []lintIssue {
	cfgPath := detectArchConfig(ctx.Root)
	if cfgPath == "" {
		return nil
	}
	if _, err := exec.LookPath("go-arch-lint"); err != nil {
		return nil
	}
	// Convert config path to absolute to ensure subprocess finds it
	absPath, err := filepath.Abs(cfgPath)
	if err != nil {
		return nil
	}
	cmd := exec.Command("go-arch-lint", "check", "--json", "--arch-file", absPath)
	cmd.Dir = ctx.Root
	out, _ := cmd.Output()
	if len(out) == 0 {
		return nil
	}
	var report struct {
		Payload struct {
			ArchWarningsDeps []struct {
				ComponentName      string `json:"ComponentName"`
				FileAbsolutePath   string `json:"FileAbsolutePath"`
				ResolvedImportName string `json:"ResolvedImportName"`
				Reference          struct {
					Line int `json:"Line"`
				} `json:"Reference"`
			} `json:"ArchWarningsDeps"`
		} `json:"Payload"`
	}
	if err := json.Unmarshal(out, &report); err != nil {
		return nil
	}
	var issues []lintIssue
	for _, v := range report.Payload.ArchWarningsDeps {
		issues = append(issues, lintIssue{
			File:     v.FileAbsolutePath,
			Rule:     "arch-violation",
			Severity: "error",
			Message: fmt.Sprintf(
				"component %s imports %s (line %d) — disallowed by go-arch-lint config",
				v.ComponentName, v.ResolvedImportName, v.Reference.Line,
			),
		})
	}
	return issues
}

func detectArchConfig(root string) string {
	for _, name := range []string{".go-arch-lint.yml", ".go-arch-lint.yaml"} {
		p := filepath.Join(root, name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}
