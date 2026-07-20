package main

import (
	"testing"

	"github.com/chrisophus/gorefactor/config"
)

func TestFilterIssuesByLintPolicy_ExcludeTestFiles(t *testing.T) {
	cfg := &config.File{Lint: config.Lint{ExcludeTestFiles: []string{"error-not-wrapped"}}}
	ctx := LintContext{Config: cfg}
	issues := []lintIssue{
		{File: "internal/foo/foo_test.go", Rule: "error-not-wrapped", Severity: "warning"},
		{File: "internal/foo/foo.go", Rule: "error-not-wrapped", Severity: "warning"},
	}
	got := filterIssuesByLintPolicy(issues, ctx)
	if len(got) != 1 || got[0].File != "internal/foo/foo.go" {
		t.Fatalf("got %+v", got)
	}
}

func TestFilterIssuesByLintPolicy_ExcludePackages(t *testing.T) {
	cfg := &config.File{Lint: config.Lint{ExcludePackages: map[string][]string{
		"high-coupling": {"internal/domain"},
	}}}
	ctx := LintContext{Config: cfg, Root: t.TempDir()}
	issues := []lintIssue{
		{
			File:     "internal/domain",
			Rule:     "high-coupling",
			Severity: "warning",
			Message:  "package internal/domain has fan-out 12 (threshold 10) — depends on too many local packages; consider consolidating or inverting dependencies",
		},
		{
			File:     "internal/wire",
			Rule:     "high-coupling",
			Severity: "warning",
			Message:  "package internal/wire has fan-out 12 (threshold 10) — depends on too many local packages; consider consolidating or inverting dependencies",
		},
	}
	got := filterIssuesByLintPolicy(issues, ctx)
	if len(got) != 1 || !containsIssueFile(got, "internal/wire") {
		t.Fatalf("got %+v", got)
	}
}

func containsIssueFile(issues []lintIssue, file string) bool {
	for _, iss := range issues {
		if iss.File == file {
			return true
		}
	}
	return false
}
