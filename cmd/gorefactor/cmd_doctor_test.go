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
func TestDoctorAutoFixStageOKDespiteFailedFixes(t *testing.T) {
	restore := swapDoctorAutoFixFn(func(string, string) (int, int, int, error) {
		return 1, 2, 5, nil
	})
	defer restore()

	stage, err := doctorAutoFixStage(".", fixLevelSafe)
	if err != nil {
		t.Fatalf("doctorAutoFixStage: %v", err)
	}
	if !stage.ok {
		t.Fatal("autofix stage should stay ok=true even when some fixes failed to apply")
	}
	if !strings.Contains(stage.info, "5 failed to apply") {
		t.Fatalf("info should report the failed count; got %q", stage.info)
	}
}

func swapDoctorAutoFixFn(fn func(string, string) (int, int, int, error)) func() {
	prev := doctorAutoFixFn
	doctorAutoFixFn = fn
	return func() { doctorAutoFixFn = prev }
}

func TestDoctorAutoFixFormatsUntouchedFile(t *testing.T) {
	dir := t.TempDir()
	writeTempGo(t, dir, "f.go", "package x\n\nfunc used() int { return 1 }\n\nfunc main() { used() }\n")
	messy := writeTempGo(t, dir, "messy.go", "package x\n\nfunc   Messy(  )   int {\n\treturn    7\n}\n")

	restore := swapVerifyGate(func(string) error { return nil })
	defer restore()

	if _, _, _, err := doctorAutoFix(dir, fixLevelSafe); err != nil {
		t.Fatalf("doctorAutoFix: %v", err)
	}
	got := readFile(t, messy)
	want := "package x\n\nfunc Messy() int {\n\treturn 7\n}\n"
	if got != want {
		t.Fatalf("messy.go should have been gofmt'd even though no lint rule touched it; got:\n%s", got)
	}
}
