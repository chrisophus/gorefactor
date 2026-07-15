package main

import (
	"path/filepath"
	"testing"
)

func TestNormalizeLintMessage(t *testing.T) {
	cases := []struct{ in, want string }{
		{"is 80 lines (threshold 75, line 98)", "is # lines (threshold #, line #)"},
		{"no digits here", "no digits here"},
		{"3-stmt block at foo.go:141-150", "#-stmt block at foo.go:#-#"},
		{"", ""},
	}
	for _, c := range cases {
		if got := normalizeLintMessage(c.in); got != c.want {
			t.Errorf("normalizeLintMessage(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestIssueFingerprint_LineIndependent(t *testing.T) {
	a := lintIssue{File: "x.go", Rule: "deep-nesting", Message: "nesting depth 5 at line 12"}
	b := lintIssue{File: "x.go", Rule: "deep-nesting", Message: "nesting depth 5 at line 40"}
	if issueFingerprint(a) != issueFingerprint(b) {
		t.Errorf("fingerprints differ across line drift:\n a=%q\n b=%q",
			issueFingerprint(a), issueFingerprint(b))
	}
	// Different rule or file must not collide.
	c := lintIssue{File: "y.go", Rule: "deep-nesting", Message: "nesting depth 5 at line 12"}
	if issueFingerprint(a) == issueFingerprint(c) {
		t.Error("fingerprints collided across different files")
	}
	d := lintIssue{File: "x.go", Rule: "complexity", Message: "nesting depth 5 at line 12"}
	if issueFingerprint(a) == issueFingerprint(d) {
		t.Error("fingerprints collided across different rules")
	}
}

func TestFilterAgainstBaseline(t *testing.T) {
	base := map[string]int{
		issueFingerprint(lintIssue{File: "x.go", Rule: "long-function", Message: "F is 80 lines"}): 1,
		issueFingerprint(lintIssue{File: "x.go", Rule: "duplicate-block", Message: "dup"}):         2,
	}
	// Same F, drifted line count -> suppressed. Two dups baselined, three now
	// -> one surfaces. A brand-new rule -> surfaces.
	issues := []lintIssue{
		{File: "x.go", Rule: "long-function", Message: "F is 92 lines"},
		{File: "x.go", Rule: "duplicate-block", Message: "dup"},
		{File: "x.go", Rule: "duplicate-block", Message: "dup"},
		{File: "x.go", Rule: "duplicate-block", Message: "dup"},
		{File: "x.go", Rule: "complexity", Message: "brand new"},
	}
	got := filterAgainstBaseline(issues, base)
	if len(got) != 2 {
		t.Fatalf("expected 2 new issues (1 extra dup + 1 complexity), got %d: %+v", len(got), got)
	}
	rules := map[string]int{}
	for _, iss := range got {
		rules[iss.Rule]++
	}
	if rules["duplicate-block"] != 1 || rules["complexity"] != 1 {
		t.Errorf("unexpected surfaced rules: %v", rules)
	}
}

func TestWriteAndLoadBaseline_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bl.json")
	issues := []lintIssue{
		{File: "a.go", Rule: "long-function", Message: "F is 80 lines (line 10)"},
		{File: "a.go", Rule: "duplicate-block", Message: "dup at a.go:1-3"},
		{File: "a.go", Rule: "duplicate-block", Message: "dup at a.go:1-3"},
	}
	if err := writeBaseline(path, issues); err != nil {
		t.Fatalf("writeBaseline: %v", err)
	}
	counts, err := loadBaseline(path)
	if err != nil {
		t.Fatalf("loadBaseline: %v", err)
	}
	// The two identical dups collapse to one fingerprint with count 2.
	if counts[issueFingerprint(issues[1])] != 2 {
		t.Errorf("expected dup count 2, got %d", counts[issueFingerprint(issues[1])])
	}
	if counts[issueFingerprint(issues[0])] != 1 {
		t.Errorf("expected long-function count 1, got %d", counts[issueFingerprint(issues[0])])
	}
	// A full round-trip through the filter should suppress everything.
	if got := filterAgainstBaseline(issues, counts); len(got) != 0 {
		t.Errorf("expected all issues suppressed on identical round-trip, got %d", len(got))
	}
}

func TestLoadBaseline_MissingFile(t *testing.T) {
	_, err := loadBaseline(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if err == nil {
		t.Fatal("expected error for missing baseline file")
	}
}
