package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// fakeFixRule is a FixableRule whose AutoFix runs an injected closure, so the
// verify/revert loop can be exercised without any real lint rule or toolchain.
type fakeFixRule struct {
	name  string
	fixFn func(iss lintIssue, ctx LintContext) error
}

func (f fakeFixRule) Name() string { return f.name }

func (f fakeFixRule) Run(LintContext) []lintIssue { return nil }

func (f fakeFixRule) AutoFix(iss lintIssue, ctx LintContext) error { return f.fixFn(iss, ctx) }

func TestDirSnapshotRestore(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.go")
	writeFileT(t, a, "package a\n\nfunc A() {}\n")

	snap, err := snapshotGoDir(dir)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	// Simulate a fix that modifies a.go, creates b.go, and deletes... nothing,
	// then also make a fresh file to prove restore removes created files.
	writeFileT(t, a, "package a\n\nfunc A() { broken }\n")
	b := filepath.Join(dir, "b.go")
	writeFileT(t, b, "package a\n\nfunc B() {}\n")

	if err := snap.restore(); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if got := readFile(t, a); got != "package a\n\nfunc A() {}\n" {
		t.Fatalf("a.go not restored: %q", got)
	}
	if _, err := os.Stat(b); !os.IsNotExist(err) {
		t.Fatalf("b.go should have been removed on restore, stat err=%v", err)
	}
}

func TestDirSnapshotRestoreRecreatesDeleted(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.go")
	orig := "package a\n\nfunc A() {}\n"
	writeFileT(t, a, orig)

	snap, err := snapshotGoDir(dir)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	// A dead-code fix that deletes the whole file's declaration could remove it.
	if err := os.Remove(a); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := snap.restore(); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if got := readFile(t, a); got != orig {
		t.Fatalf("a.go not recreated: %q", got)
	}
}

func TestApplyAutoFixesVerifyRevertsOnRedGate(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.go")
	orig := "package a\n\nfunc A() {}\n"
	writeFileT(t, file, orig)

	rule := fakeFixRule{name: "fake", fixFn: func(iss lintIssue, _ LintContext) error {
		return os.WriteFile(iss.File, []byte("package a\n\nfunc A() { broken }\n"), 0644)
	}}
	iss := lintIssue{File: file, Rule: "fake", AutoFixCmd: "fake"}

	restore := swapVerifyGate(func(string) error { return fmt.Errorf("boom") })
	defer restore()

	applied, reverted, failed := applyAutoFixes([]lintIssue{iss},
		LintContext{Root: dir}, []LintRule{rule}, true)
	if applied != 0 || reverted != 1 || failed != 0 {
		t.Fatalf("counts: applied=%d reverted=%d failed=%d", applied, reverted, failed)
	}
	if got := readFile(t, file); got != orig {
		t.Fatalf("fix not reverted after red gate: %q", got)
	}
}

func TestApplyAutoFixesVerifyKeepsOnGreenGate(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.go")
	writeFileT(t, file, "package a\n\nfunc A() {}\n")
	fixed := "package a\n\nfunc A() { _ = 1 }\n"

	rule := fakeFixRule{name: "fake", fixFn: func(iss lintIssue, _ LintContext) error {
		return os.WriteFile(iss.File, []byte(fixed), 0644)
	}}
	iss := lintIssue{File: file, Rule: "fake", AutoFixCmd: "fake"}

	restore := swapVerifyGate(func(string) error { return nil })
	defer restore()

	applied, reverted, failed := applyAutoFixes([]lintIssue{iss},
		LintContext{Root: dir}, []LintRule{rule}, true)
	if applied != 1 || reverted != 0 || failed != 0 {
		t.Fatalf("counts: applied=%d reverted=%d failed=%d", applied, reverted, failed)
	}
	if got := readFile(t, file); got != fixed {
		t.Fatalf("fix not kept after green gate: %q", got)
	}
}

// TestApplyAutoFixesVerifyKeepsGoodRevertsBad proves per-fix attribution: a
// good fix in one package is kept even though a later fix in another package
// is reverted. The gate fails only while the bad file contains its marker.
func TestApplyAutoFixesVerifyKeepsGoodRevertsBad(t *testing.T) {
	root := t.TempDir()
	goodDir := filepath.Join(root, "good")
	badDir := filepath.Join(root, "bad")
	if err := os.MkdirAll(goodDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(badDir, 0755); err != nil {
		t.Fatal(err)
	}
	goodFile := filepath.Join(goodDir, "g.go")
	badFile := filepath.Join(badDir, "b.go")
	goodOrig := "package good\n\nfunc G() {}\n"
	badOrig := "package bad\n\nfunc B() {}\n"
	writeFileT(t, goodFile, goodOrig)
	writeFileT(t, badFile, badOrig)

	goodFixed := "package good\n\nfunc G() { _ = 1 }\n"
	badMarker := "package bad\n\nfunc B() { POISON }\n"

	rule := fakeFixRule{name: "fake", fixFn: func(iss lintIssue, _ LintContext) error {
		if filepath.Dir(iss.File) == goodDir {
			return os.WriteFile(iss.File, []byte(goodFixed), 0644)
		}
		return os.WriteFile(iss.File, []byte(badMarker), 0644)
	}}
	// Gate fails iff the bad file currently contains the poison marker.
	restore := swapVerifyGate(func(string) error {
		if readFileNoT(badFile) == badMarker {
			return fmt.Errorf("bad package broken")
		}
		return nil
	})
	defer restore()

	issues := []lintIssue{
		{File: goodFile, Rule: "fake", AutoFixCmd: "fake"},
		{File: badFile, Rule: "fake", AutoFixCmd: "fake"},
	}
	applied, reverted, failed := applyAutoFixes(issues, LintContext{Root: root}, []LintRule{rule}, true)
	if applied != 1 || reverted != 1 || failed != 0 {
		t.Fatalf("counts: applied=%d reverted=%d failed=%d", applied, reverted, failed)
	}
	if got := readFile(t, goodFile); got != goodFixed {
		t.Fatalf("good fix should be kept: %q", got)
	}
	if got := readFile(t, badFile); got != badOrig {
		t.Fatalf("bad fix should be reverted: %q", got)
	}
}

func writeFileT(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func swapVerifyGate(fn func(string) error) func() {
	prev := verifyGateFn
	verifyGateFn = fn
	return func() { verifyGateFn = prev }
}

func readFileNoT(path string) string {
	b, _ := os.ReadFile(path)
	return string(b)
}
