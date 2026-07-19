package main

import (
	"testing"
)

func TestParseLintOptions_RuleFlags(t *testing.T) {
	opts, err := parseLintOptions([]string{
		".",
		"--rule", "untested-package",
		"--skip-rule", "golangci-lint",
		"--fail-on", "warning",
		"--fail-only",
		"--max", "500",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !opts.onlyRules["untested-package"] {
		t.Fatal("expected --rule untested-package")
	}
	if !opts.skipRules["golangci-lint"] {
		t.Fatal("expected --skip-rule golangci-lint")
	}
	if opts.failOn != "warning" {
		t.Fatalf("failOn = %q, want warning", opts.failOn)
	}
	if !opts.failOnly {
		t.Fatal("expected --fail-only to be enabled")
	}
	if opts.maxSize != 500 {
		t.Fatalf("maxSize = %d, want 500", opts.maxSize)
	}
}

func TestFilterLintRules_OnlyAndSkip(t *testing.T) {
	all := defaultLintRules()
	opts := lintOptions{
		onlyRules: map[string]bool{"file-size": true, "dead-code": true},
		skipRules: map[string]bool{"dead-code": true},
	}
	got := filterLintRules(all, opts)
	if len(got) != 1 || got[0].Name() != "file-size" {
		t.Fatalf("filter = %v, want [file-size]", ruleNames(got))
	}
}

func TestLintShouldFail(t *testing.T) {
	issues := []lintIssue{{Severity: "warning"}}
	if !lintShouldFail(issues, "warning") {
		t.Fatal("expected fail on warning")
	}
	if lintShouldFail(issues, "error") {
		t.Fatal("did not expect fail on error-only gate")
	}
	issues = append(issues, lintIssue{Severity: "error"})
	if !lintShouldFail(issues, "error") {
		t.Fatal("expected fail on error")
	}
	// info is advisory: it must never fail a gate, not even a warning gate.
	if lintShouldFail([]lintIssue{{Severity: "info"}}, "warning") {
		t.Fatal("info finding must not fail a warning gate")
	}
}

func TestFailingIssueCount(t *testing.T) {
	issues := []lintIssue{
		{Severity: "info"},
		{Severity: "warning"},
		{Severity: "error"},
		{Severity: "error"},
	}
	if got := failingIssueCount(issues, "error"); got != 2 {
		t.Fatalf("failingIssueCount(error) = %d, want 2", got)
	}
	// warning gate counts warning+error but not the advisory info finding.
	if got := failingIssueCount(issues, "warning"); got != 3 {
		t.Fatalf("failingIssueCount(warning) = %d, want 3 (info excluded)", got)
	}
}

func TestFailingIssues(t *testing.T) {
	issues := []lintIssue{
		{Severity: "info"},
		{Severity: "warning"},
		{Severity: "error"},
		{Severity: "error"},
	}
	if got := failingIssues(issues, "error"); len(got) != 2 {
		t.Fatalf("len(failingIssues(error)) = %d, want 2", len(got))
	}
	// warning gate keeps warning+error, drops the advisory info finding.
	if got := failingIssues(issues, "warning"); len(got) != 3 {
		t.Fatalf("len(failingIssues(warning)) = %d, want 3 (info excluded)", len(got))
	}
}

func ruleNames(rules []LintRule) []string {
	out := make([]string, len(rules))
	for i, r := range rules {
		out[i] = r.Name()
	}
	return out
}
