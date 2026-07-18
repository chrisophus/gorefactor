package main

import (
	"testing"

	"github.com/chrisophus/gorefactor/analyzer"
	"github.com/chrisophus/gorefactor/doctor"
)

func TestDefaultLintRules_ExpectedSet(t *testing.T) {
	want := []string{
		"file-size",
		"extract-candidate",
		"complexity",
		"long-function",
		"deep-nesting",
		"error-not-wrapped",
		"high-coupling",
		"high-blast-radius",
		"premature-abstraction",
		"if-err-log-return",
		"wrap-log-return",
		"wrap-bridge-log-return",
		"duplicate-bare-sentinel",
		"funcorder-constructor",
		"funcorder-struct-method",
		"funcorder-function",
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
		"untested-function",
		"vacuous-test",
		"sleep-in-test",
		"fatal-in-library",
		"string-concat-in-loop",
		"linear-search-in-loop",
		"unstopped-ticker",
		"naked-goroutine",
		"pass-through-param",
		"regexp-compile-in-func",
		"low-gorefactor-adherence",
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

func TestFixableRule_ExpectedSet(t *testing.T) {
	want := map[string]bool{
		"file-size": true,
		// complexity extraction autofix stays disabled (unreliable). long-function's
		// is aggressive-only + verify-gated now that the extractor handles
		// return-lifting tails and type-switch bindings.
		"long-function":           true,
		"extract-candidate":       true,
		"dead-code":               true,
		"error-not-wrapped":       true,
		"if-err-log-return":       true,
		"wrap-log-return":         true,
		"wrap-bridge-log-return":  true,
		"duplicate-bare-sentinel": true,
		"funcorder-constructor":   true,
		"funcorder-struct-method": true,
		"funcorder-function":      true,
		"regexp-compile-in-func":  true,
	}
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

func TestScoreClassifiedRulesExist(t *testing.T) {
	known := map[string]bool{}
	for _, r := range defaultLintRules() {
		known[r.Name()] = true
	}
	for _, name := range doctor.ScoreClassifiedRules() {
		if !known[name] {
			t.Errorf("score layer classifies %q, which is not a registered lint rule", name)
		}
	}
}
