package main

import (
	"testing"

	"github.com/chrisophus/gorefactor/analyzer"
)

func TestDefaultLintRules_ExpectedSet(t *testing.T) {
	want := []string{
		"file-size",
		"extract-candidate",
		"complexity",
		"god-object",
		"excessive-params",
		"excessive-returns",
		"fat-interface",
		"large-class",
		"data-clumps",
		"type-switch",
		"duplicate-block",
		"dead-code",
		"untested-package",
		"golangci-lint",
	}
	got := defaultLintRules()
	if len(got) != len(want) {
		t.Fatalf("len(defaultLintRules()) = %d, want %d", len(got), len(want))
	}
	for i, r := range got {
		if r.Name() != want[i] {
			t.Errorf("rule[%d].Name() = %q, want %q", i, r.Name(), want[i])
		}
	}
}

func TestDefaultLintRules_NamesUnique(t *testing.T) {
	seen := make(map[string]bool)
	for _, r := range defaultLintRules() {
		if seen[r.Name()] {
			t.Errorf("duplicate rule name: %s", r.Name())
		}
		seen[r.Name()] = true
	}
}

// TestLintRules_RuleFieldMatchesName runs every rule against the repo and
// asserts that every emitted issue carries Rule == rule.Name(). This is the
// load-bearing invariant the dispatcher relies on for autofix routing.
func TestLintRules_RuleFieldMatchesName(t *testing.T) {
	files, err := analyzer.WalkGoFiles("../..", analyzer.DefaultWalkOptions())
	if err != nil {
		t.Fatal(err)
	}
	ctx := LintContext{Root: "../..", Files: files, MaxSize: 600}
	for _, rule := range defaultLintRules() {
		for _, iss := range rule.Run(ctx) {
			if iss.Rule != rule.Name() {
				t.Errorf("rule %s emitted issue with Rule=%q (file=%s msg=%s)", rule.Name(), iss.Rule, iss.File, iss.Message)
			}
		}
	}
}

func TestFixableRule_OnlyFileSizeAndDeadCode(t *testing.T) {
	want := map[string]bool{"file-size": true, "dead-code": true}
	got := make(map[string]bool)
	for _, r := range defaultLintRules() {
		if _, ok := r.(FixableRule); ok {
			got[r.Name()] = true
		}
	}
	if len(got) != len(want) {
		t.Errorf("FixableRule rules = %v, want %v", got, want)
	}
	for name := range want {
		if !got[name] {
			t.Errorf("rule %s should implement FixableRule but does not", name)
		}
	}
}
