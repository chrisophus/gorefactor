package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestDoctorAutoFixAppliesDeadCode(t *testing.T) {
	dir := t.TempDir()
	path := writeTempGo(t, dir, "f.go", "package x\n\nfunc used() int { return 1 }\n\nfunc main() { used() }\n\nfunc unused() int { return 2 }\n")

	restore := swapVerifyGate(func(string) error { return nil })
	defer restore()

	applied, reverted, failed, err := doctorAutoFix(dir, fixLevelSafe)
	if err != nil {
		t.Fatalf("doctorAutoFix: %v", err)
	}
	if applied != 1 || reverted != 0 || failed != 0 {
		t.Fatalf("counts: applied=%d reverted=%d failed=%d", applied, reverted, failed)
	}
	got := readFile(t, path)
	if strings.Contains(got, "func unused") {
		t.Fatalf("unused should have been deleted; got:\n%s", got)
	}
	if !strings.Contains(got, "func used") {
		t.Fatalf("used should have remained; got:\n%s", got)
	}
}

func TestDoctorAutoFixRevertsOnRedGate(t *testing.T) {
	dir := t.TempDir()
	path := writeTempGo(t, dir, "f.go", "package x\n\nfunc used() int { return 1 }\n\nfunc main() { used() }\n\nfunc unused() int { return 2 }\n")
	orig := readFile(t, path)

	restore := swapVerifyGate(func(string) error { return fmt.Errorf("boom") })
	defer restore()

	applied, reverted, failed, err := doctorAutoFix(dir, fixLevelSafe)
	if err != nil {
		t.Fatalf("doctorAutoFix: %v", err)
	}
	if applied != 0 || reverted != 1 || failed != 0 {
		t.Fatalf("counts: applied=%d reverted=%d failed=%d", applied, reverted, failed)
	}
	if got := readFile(t, path); got != orig {
		t.Fatalf("fix should have been reverted; got:\n%s", got)
	}
}

func TestDoctorCommandRejectsBadFixLevel(t *testing.T) {
	dir := t.TempDir()
	if err := doctorCommand([]string{dir, "--fix", "--fix-level", "bogus"}); err == nil {
		t.Fatal("expected error for invalid --fix-level")
	}
}
