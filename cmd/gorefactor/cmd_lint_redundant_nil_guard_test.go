package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRedundantNilGuardRule_FiresWhenCallersProve(t *testing.T) {
	dir := t.TempDir()
	src := `package p

type T struct{ N int }

func use(t *T) int {
	if t == nil {
		return 0
	}
	return t.N
}

func callerA() int {
	x := &T{N: 1}
	return use(x)
}

func callerB(t *T) int {
	if t == nil {
		return -1
	}
	return use(t)
}
`
	path := filepath.Join(dir, "a.go")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	issues := redundantNilGuardRule{}.Run(LintContext{Files: []string{path}})
	if len(issues) != 1 {
		t.Fatalf("issues = %+v, want 1 redundant-nil-guard", issues)
	}
	if issues[0].Rule != "redundant-nil-guard" {
		t.Fatalf("rule = %q", issues[0].Rule)
	}
	if !strings.Contains(issues[0].Message, "use") {
		t.Fatalf("message = %q", issues[0].Message)
	}
}

func TestRedundantNilGuardRule_QuietWhenAnyCallerUnproven(t *testing.T) {
	dir := t.TempDir()
	src := `package p

type T struct{ N int }

func use(t *T) int {
	if t == nil {
		return 0
	}
	return t.N
}

func safe() int { return use(&T{N: 1}) }

func unsafe(t *T) int { return use(t) }
`
	path := filepath.Join(dir, "a.go")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	issues := redundantNilGuardRule{}.Run(LintContext{Files: []string{path}})
	if len(issues) != 0 {
		t.Fatalf("must not fire when any caller is unproven: %+v", issues)
	}
}

func TestRedundantNilGuardRule_SkipsExported(t *testing.T) {
	dir := t.TempDir()
	src := `package p

type T struct{ N int }

func Use(t *T) int {
	if t == nil {
		return 0
	}
	return t.N
}

func caller() int { return Use(&T{N: 1}) }
`
	path := filepath.Join(dir, "a.go")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	issues := redundantNilGuardRule{}.Run(LintContext{Files: []string{path}})
	if len(issues) != 0 {
		t.Fatalf("exported funcs are out of scope: %+v", issues)
	}
}

func TestRedundantNilGuardRule_SecondGuardInPrologue(t *testing.T) {
	dir := t.TempDir()
	src := `package p

type T struct{ N int }

func use(a, b *T) int {
	if a == nil {
		return 0
	}
	if b == nil {
		return 0
	}
	return a.N + b.N
}

func caller() int {
	return use(&T{N: 1}, &T{N: 2})
}
`
	path := filepath.Join(dir, "a.go")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	issues := redundantNilGuardRule{}.Run(LintContext{Files: []string{path}})
	if len(issues) != 2 {
		t.Fatalf("issues = %+v, want guards on both a and b flagged", issues)
	}
}

func TestRedundantNilGuardRule_CombinedOrGuard(t *testing.T) {
	dir := t.TempDir()
	src := `package p

type T struct{ N int }

func use(a, b *T) int {
	if a == nil || b == nil {
		return 0
	}
	return a.N + b.N
}

func caller(a, b *T) int {
	if a == nil || b == nil {
		return -1
	}
	return use(a, b)
}
`
	path := filepath.Join(dir, "a.go")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	issues := redundantNilGuardRule{}.Run(LintContext{Files: []string{path}})
	if len(issues) != 2 {
		t.Fatalf("issues = %+v, want the combined guard flagged for both params", issues)
	}
}

func TestRedundantNilGuardRule_MixedGuardQuiet(t *testing.T) {
	dir := t.TempDir()
	src := `package p

type T struct{ N int }

func broken() bool { return false }

func use(t *T) int {
	if t == nil || broken() {
		return 0
	}
	return t.N
}

func caller() int { return use(&T{N: 1}) }
`
	path := filepath.Join(dir, "a.go")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	issues := redundantNilGuardRule{}.Run(LintContext{Files: []string{path}})
	if len(issues) != 0 {
		t.Fatalf("mixed guards do more than nil-check; must stay quiet: %+v", issues)
	}
}

func TestRedundantNilGuardRule_QuietWhenCallerReassignsAfterGuard(t *testing.T) {
	// Modeled on cmd/compile's implements(): the caller nil-rejects t at
	// entry but then reassigns it from a call that may return nil, so the
	// callee's guard is load-bearing.
	dir := t.TempDir()
	src := `package p

type T struct{ N int }

func derive(t *T) *T { return nil }

func use(t *T) int {
	if t == nil {
		return 0
	}
	return t.N
}

func caller(t *T) int {
	if t == nil {
		return -1
	}
	t = derive(t)
	return use(t)
}
`
	path := filepath.Join(dir, "a.go")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	issues := redundantNilGuardRule{}.Run(LintContext{Files: []string{path}})
	if len(issues) != 0 {
		t.Fatalf("guard proof must not survive a reassignment: %+v", issues)
	}
}

func TestRedundantNilGuardRule_QuietWhenCallerClosureMutates(t *testing.T) {
	dir := t.TempDir()
	src := `package p

type T struct{ N int }

func use(t *T) int {
	if t == nil {
		return 0
	}
	return t.N
}

func caller(t *T) int {
	if t == nil {
		return -1
	}
	f := func() { t = nil }
	f()
	return use(t)
}
`
	path := filepath.Join(dir, "a.go")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	issues := redundantNilGuardRule{}.Run(LintContext{Files: []string{path}})
	if len(issues) != 0 {
		t.Fatalf("guard proof must not survive a closure mutation: %+v", issues)
	}
}

func TestRedundantNilGuardRule_NewAndAmpersand(t *testing.T) {
	dir := t.TempDir()
	src := `package p

type T struct{ N int }

func use(t *T) int {
	if t == nil {
		return 0
	}
	return t.N
}

func a() int { return use(new(T)) }
func b() int { return use(&T{}) }
`
	path := filepath.Join(dir, "a.go")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	issues := redundantNilGuardRule{}.Run(LintContext{Files: []string{path}})
	if len(issues) != 1 {
		t.Fatalf("issues = %+v, want 1", issues)
	}
}
