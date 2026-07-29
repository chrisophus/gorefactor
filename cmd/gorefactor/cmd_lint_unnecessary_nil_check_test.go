package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnnecessaryNilCheck_ConstructedThenChecked(t *testing.T) {
	issues := runUnnecessaryNilCheck(t, `package p

type T struct{ N int }

func a() int {
	x := &T{N: 1}
	if x == nil {
		return 0
	}
	return x.N
}

func b() int {
	x := new(T)
	if x == nil {
		return 0
	}
	return x.N
}

func c() int {
	m := make(map[string]int)
	if m == nil {
		return 0
	}
	return len(m)
}
`)
	wantIssueCount(t, issues, 3)
	if !strings.Contains(issues[0].Message, "non-nil value") {
		t.Fatalf("message = %q", issues[0].Message)
	}
}

func TestUnnecessaryNilCheck_DuplicateGuard(t *testing.T) {
	issues := runUnnecessaryNilCheck(t, `package p

type T struct{ N int }

func f(x *T) int {
	if x == nil {
		return 0
	}
	y := x.N + 1
	if x == nil {
		return y
	}
	return x.N
}
`)
	wantIssueCount(t, issues, 1)
	if !strings.Contains(issues[0].Message, "already returned on nil") {
		t.Fatalf("message = %q", issues[0].Message)
	}
}

func TestUnnecessaryNilCheck_NestedInsideGuard(t *testing.T) {
	issues := runUnnecessaryNilCheck(t, `package p

type T struct{ N int }

func f(x *T) int {
	if x != nil {
		if x == nil {
			return -1
		}
		return x.N
	}
	return 0
}
`)
	wantIssueCount(t, issues, 1)
	if !strings.Contains(issues[0].Message, "already establishes x != nil") {
		t.Fatalf("message = %q", issues[0].Message)
	}
}

func TestUnnecessaryNilCheck_ElseBranchOfNilTest(t *testing.T) {
	issues := runUnnecessaryNilCheck(t, `package p

type T struct{ N int }

func f(x *T) int {
	if x == nil {
		return 0
	} else if x != nil {
		return x.N
	}
	return 1
}
`)
	wantIssueCount(t, issues, 1)
}

func TestUnnecessaryNilCheck_LenComboSliceAndMap(t *testing.T) {
	issues := runUnnecessaryNilCheck(t, `package p

func f(xs []int) int {
	if xs != nil && len(xs) > 0 {
		return xs[0]
	}
	return 0
}

func g(m map[string]int) bool {
	return m == nil || len(m) == 0
}
`)
	wantIssueCount(t, issues, 2)
	if !strings.Contains(issues[0].Message, "len(nil) == 0") {
		t.Fatalf("message = %q", issues[0].Message)
	}
}

func TestUnnecessaryNilCheck_LenComboUnknownTypeQuiet(t *testing.T) {
	// x's type is unknown (returned by a call) — len-combo must not fire.
	issues := runUnnecessaryNilCheck(t, `package p

func from() []int { return nil }

func f() int {
	x := from()
	if x != nil && len(x) > 0 {
		return x[0]
	}
	return 0
}
`)
	wantIssueCount(t, issues, 0)
}

func TestUnnecessaryNilCheck_FiresAcrossMethodCallOnPointer(t *testing.T) {
	issues := runUnnecessaryNilCheck(t, `package p

type T struct{ N int }

func (t *T) Bump() { t.N++ }

func f(x *T) int {
	if x == nil {
		return 0
	}
	x.Bump()
	if x == nil {
		return -1
	}
	return x.N
}
`)
	wantIssueCount(t, issues, 1)
}

func TestUnnecessaryNilCheck_QuietAfterMethodCallOnUnknownKind(t *testing.T) {
	// S may be a named slice with a pointer-receiver method that rebinds the
	// value through the implicit &x — the fact must not survive x.Reset().
	issues := runUnnecessaryNilCheck(t, `package p

type S []int

func (s *S) Reset() { *s = nil }

func f(x S) int {
	if x == nil {
		return 0
	}
	x.Reset()
	if x == nil {
		return -1
	}
	return len(x)
}
`)
	wantIssueCount(t, issues, 0)
}

func TestUnnecessaryNilCheck_QuietOnReassign(t *testing.T) {
	issues := runUnnecessaryNilCheck(t, `package p

type T struct{ N int }

func from() *T { return nil }

func f(x *T) int {
	if x == nil {
		return 0
	}
	x = from()
	if x == nil {
		return -1
	}
	return x.N
}
`)
	wantIssueCount(t, issues, 0)
}

func TestUnnecessaryNilCheck_QuietOnClosureAssign(t *testing.T) {
	// The closure may run between the guard and the second check.
	issues := runUnnecessaryNilCheck(t, `package p

type T struct{ N int }

func f(x *T) int {
	reset := func() { x = nil }
	if x == nil {
		return 0
	}
	reset()
	if x == nil {
		return -1
	}
	return x.N
}
`)
	wantIssueCount(t, issues, 0)
}

func TestUnnecessaryNilCheck_QuietOnAddressTaken(t *testing.T) {
	issues := runUnnecessaryNilCheck(t, `package p

type T struct{ N int }

func clobber(p **T) { *p = nil }

func f(x *T) int {
	if x == nil {
		return 0
	}
	clobber(&x)
	if x == nil {
		return -1
	}
	return x.N
}
`)
	wantIssueCount(t, issues, 0)
}

func TestUnnecessaryNilCheck_QuietInLoopWithLaterAssign(t *testing.T) {
	// x is reassigned at the bottom of the loop body, so the check at the top
	// is live on every iteration after the first.
	issues := runUnnecessaryNilCheck(t, `package p

type T struct{ N int }

func from() *T { return nil }

func f() int {
	x := &T{N: 1}
	total := 0
	for i := 0; i < 3; i++ {
		if x == nil {
			return total
		}
		total += x.N
		x = from()
	}
	return total
}
`)
	wantIssueCount(t, issues, 0)
}

func TestUnnecessaryNilCheck_QuietOnGlobal(t *testing.T) {
	// g is package-level: any call between the two checks may reassign it.
	issues := runUnnecessaryNilCheck(t, `package p

type T struct{ N int }

var g *T

func mutate() { g = nil }

func f() int {
	if g == nil {
		return 0
	}
	mutate()
	if g == nil {
		return -1
	}
	return g.N
}
`)
	wantIssueCount(t, issues, 0)
}

func TestUnnecessaryNilCheck_QuietWhenGuardHasElse(t *testing.T) {
	// With an else branch the fall-through path is not nil-rejecting in the
	// simple prologue sense; the rule deliberately stays quiet.
	issues := runUnnecessaryNilCheck(t, `package p

type T struct{ N int }

func h() {}

func f(x *T) int {
	if x == nil {
		return 0
	} else {
		h()
	}
	if x == nil {
		return -1
	}
	return x.N
}
`)
	wantIssueCount(t, issues, 0)
}

func TestUnnecessaryNilCheck_MixedDisjunctGuardStillRejects(t *testing.T) {
	// `err != nil || x == nil` falling through proves x non-nil even though
	// the guard also tests err.
	issues := runUnnecessaryNilCheck(t, `package p

type T struct{ N int }

func f(x *T, err error) int {
	if err != nil || x == nil {
		return 0
	}
	if x == nil {
		return -1
	}
	return x.N
}
`)
	wantIssueCount(t, issues, 1)
}

func TestUnnecessaryNilCheck_SkipsGotoFunctions(t *testing.T) {
	issues := runUnnecessaryNilCheck(t, `package p

type T struct{ N int }

func f(x *T) int {
	if x == nil {
		return 0
	}
loop:
	if x == nil {
		goto loop
	}
	return x.N
}
`)
	wantIssueCount(t, issues, 0)
}

func runUnnecessaryNilCheck(t *testing.T, src string) []lintIssue {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "a.go")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return unnecessaryNilCheckRule{}.Run(LintContext{Files: []string{path}})
}

func wantIssueCount(t *testing.T, issues []lintIssue, n int) {
	t.Helper()
	if len(issues) != n {
		t.Fatalf("issues = %+v, want %d", issues, n)
	}
	for _, iss := range issues {
		if iss.Rule != "unnecessary-nil-check" {
			t.Fatalf("rule = %q", iss.Rule)
		}
	}
}
