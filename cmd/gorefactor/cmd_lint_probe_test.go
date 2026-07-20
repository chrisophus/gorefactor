package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// Probe mode: the fix applies, the gate passes, the tree is restored and
// the outcome is journaled as verified.
func TestProbeVerifiesAndRestores(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "p")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "t.go")
	const original = "package p\n\nfunc F() {}\n"
	writeFileT(t, target, original)

	iss := outcomeFixIssue("dead-code", target)
	var calls []string
	rule := scriptedFixRule{name: "dead-code", fixErr: map[string]error{}, fixCalls: &calls,
		mutate: func(file string) { writeFileT(t, file, "package p\n") }}
	restore := swapVerifyGate(func(string) error { return nil })
	defer restore()

	verified, wouldRevert, failed := applyAutoFixes([]lintIssue{iss}, LintContext{Root: root}, []LintRule{rule}, true, true)
	if verified != 1 || wouldRevert != 0 || failed != 0 {
		t.Fatalf("counts = %d/%d/%d, want 1 verified", verified, wouldRevert, failed)
	}
	if got := readFileNoT(target); got != original {
		t.Fatalf("probe must restore the tree, file now:\n%s", got)
	}
	if oc := loadAutofixOutcomes(root)[issueFingerprint(iss)]; oc != outcomeVerified {
		t.Fatalf("outcome = %q, want verified", oc)
	}
}

// Probe mode with a red gate journals reverted, and the tree is restored.
func TestProbeRecordsWouldRevert(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "p")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "t.go")
	const original = "package p\n\nfunc F() {}\n"
	writeFileT(t, target, original)

	iss := outcomeFixIssue("dead-code", target)
	var calls []string
	rule := scriptedFixRule{name: "dead-code", fixErr: map[string]error{}, fixCalls: &calls,
		mutate: func(file string) { writeFileT(t, file, "package p\n") }}
	restore := swapVerifyGate(func(string) error { return errors.New("tests failed") })
	defer restore()

	verified, wouldRevert, failed := applyAutoFixes([]lintIssue{iss}, LintContext{Root: root}, []LintRule{rule}, true, true)
	if verified != 0 || wouldRevert != 1 || failed != 0 {
		t.Fatalf("counts = %d/%d/%d, want 1 would-revert", verified, wouldRevert, failed)
	}
	if got := readFileNoT(target); got != original {
		t.Fatalf("tree must be restored, file now:\n%s", got)
	}
	if oc := loadAutofixOutcomes(root)[issueFingerprint(iss)]; oc != outcomeReverted {
		t.Fatalf("outcome = %q, want reverted", oc)
	}
}

// A live note is not clobbered by a journal outcome for the same finding.
func TestAnnotateDoesNotOverwriteLiveNote(t *testing.T) {
	root := t.TempDir()
	iss := lintIssue{File: "p/l.go", Rule: "extract-candidate", Severity: "warning",
		Message: "f is 90 lines (threshold 75, line 1) — consider extracting", Note: "live note"}
	appendAutofixOutcomes(root, []autofixOutcome{recordOutcome(iss, outcomeReverted, "x")})
	issues := []lintIssue{iss}
	annotateIssuesWithOutcomes(root, issues)
	if issues[0].Note != "live note" {
		t.Fatalf("note = %q, live note must win", issues[0].Note)
	}
}
