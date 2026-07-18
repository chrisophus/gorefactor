package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// scriptedFixRule is a FixableRule whose fix outcome is scripted per file.
type scriptedFixRule struct {
	name     string
	fixErr   map[string]error // file -> AutoFix result (nil = success)
	fixCalls *[]string        // records files AutoFix was invoked for
}

func (r scriptedFixRule) Name() string                { return r.name }
func (r scriptedFixRule) Run(LintContext) []lintIssue { return nil }
func (r scriptedFixRule) AutoFix(iss lintIssue, _ LintContext) error {
	*r.fixCalls = append(*r.fixCalls, iss.File)
	return r.fixErr[iss.File]
}

func outcomeFixIssue(rule, file string) lintIssue {
	return lintIssue{File: file, Rule: rule, Severity: "warning",
		Message: "x is dead (line 3)", AutoFixCmd: "gorefactor whatever"}
}

// Outcomes append and reload; the latest record per fingerprint wins.
func TestAutofixOutcomesRoundTripLatestWins(t *testing.T) {
	root := t.TempDir()
	iss := outcomeFixIssue("dead-code", "a/b.go")
	appendAutofixOutcomes(root, []autofixOutcome{recordOutcome(iss, outcomeApplied, "")})
	appendAutofixOutcomes(root, []autofixOutcome{recordOutcome(iss, outcomeReverted, "boom")})
	got := loadAutofixOutcomes(root)
	if got[issueFingerprint(iss)] != outcomeReverted {
		t.Fatalf("latest outcome should win, got %q", got[issueFingerprint(iss)])
	}
}

// A gate-reverted dead-code deletion falsifies the finding: demote to info
// with an explanatory note. Other reverted rules keep severity but say what
// happened; a no-target extraction retires the extraction promise.
func TestAnnotateIssuesWithOutcomes(t *testing.T) {
	root := t.TempDir()
	dead := outcomeFixIssue("dead-code", "p/dead.go")
	wrap := lintIssue{File: "p/w.go", Rule: "error-not-wrapped", Severity: "warning", Message: "bare err at line 9"}
	long := lintIssue{File: "p/l.go", Rule: "long-function", Severity: "warning", Message: "f is 90 lines (threshold 75, line 1) — consider extracting"}
	clean := lintIssue{File: "p/c.go", Rule: "duplicate-block", Severity: "warning", Message: "dup"}
	appendAutofixOutcomes(root, []autofixOutcome{
		recordOutcome(dead, outcomeReverted, "build broke"),
		recordOutcome(wrap, outcomeReverted, "test broke"),
		recordOutcome(long, outcomeNoTarget, "no extractable top-level blocks"),
	})

	issues := []lintIssue{dead, wrap, long, clean}
	annotateIssuesWithOutcomes(root, issues)

	if issues[0].Severity != "info" || !strings.Contains(issues[0].Note, "reachable") {
		t.Errorf("dead-code: severity=%q note=%q, want info + reachability note", issues[0].Severity, issues[0].Note)
	}
	if issues[1].Severity != "warning" || !strings.Contains(issues[1].Note, "contractual") {
		t.Errorf("error-not-wrapped: severity=%q note=%q, want warning + contractual note", issues[1].Severity, issues[1].Note)
	}
	if !strings.Contains(issues[2].Note, "restructuring") {
		t.Errorf("long-function note = %q, want needs-restructuring", issues[2].Note)
	}
	if issues[3].Note != "" || issues[3].Severity != "warning" {
		t.Errorf("untouched issue changed: %+v", issues[3])
	}
	// The note must not shift the fingerprint, or the journal key drifts.
	if issueFingerprint(issues[0]) != issueFingerprint(dead) {
		t.Error("annotation changed the fingerprint")
	}
}

// A fix the gate already rejected is not re-attempted.
func TestApplyAutoFixesSkipsKnownReverted(t *testing.T) {
	root := t.TempDir()
	skippedIss := outcomeFixIssue("dead-code", filepath.Join(root, "s.go"))
	freshIss := outcomeFixIssue("dead-code", filepath.Join(root, "f.go"))
	appendAutofixOutcomes(root, []autofixOutcome{recordOutcome(skippedIss, outcomeReverted, "gate red")})

	var calls []string
	rule := scriptedFixRule{name: "dead-code", fixErr: map[string]error{}, fixCalls: &calls}
	ctx := LintContext{Root: root}
	applied, reverted, failed := applyAutoFixes([]lintIssue{skippedIss, freshIss}, ctx, []LintRule{rule}, false)

	if len(calls) != 1 || calls[0] != freshIss.File {
		t.Fatalf("AutoFix calls = %v, want only the fresh issue", calls)
	}
	if applied != 1 || reverted != 0 || failed != 1 {
		t.Fatalf("counts = %d/%d/%d, want 1 applied, 0 reverted, 1 failed(skip)", applied, reverted, failed)
	}
	if got := loadAutofixOutcomes(root)[issueFingerprint(freshIss)]; got != outcomeApplied {
		t.Fatalf("fresh issue outcome = %q, want applied", got)
	}
}

// The bisect path journals reverted and no-target outcomes.
func TestBisectRecordsOutcomes(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "p")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	bad := outcomeFixIssue("dead-code", filepath.Join(dir, "bad.go"))
	noTgt := outcomeFixIssue("dead-code", filepath.Join(dir, "gone.go"))
	writeFileT(t, bad.File, "package p\n")
	writeFileT(t, noTgt.File, "package p\n")

	var calls []string
	rule := scriptedFixRule{name: "dead-code",
		fixErr: map[string]error{noTgt.File: errors.New("no such decl")}, fixCalls: &calls}
	restore := swapVerifyGate(func(string) error { return errors.New("tests failed") })
	defer restore()

	applied, reverted, failed := applyAutoFixes([]lintIssue{bad, noTgt}, LintContext{Root: root}, []LintRule{rule}, true)
	if applied != 0 || reverted != 1 || failed != 1 {
		t.Fatalf("counts = %d/%d/%d, want 0 applied, 1 reverted, 1 failed", applied, reverted, failed)
	}
	oc := loadAutofixOutcomes(root)
	if oc[issueFingerprint(bad)] != outcomeReverted {
		t.Errorf("bad fix outcome = %q, want reverted", oc[issueFingerprint(bad)])
	}
	if oc[issueFingerprint(noTgt)] != outcomeNoTarget {
		t.Errorf("no-target outcome = %q, want no_target", oc[issueFingerprint(noTgt)])
	}
}
