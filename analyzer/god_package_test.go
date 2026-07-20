package analyzer

import (
	"fmt"
	"strings"
	"testing"
)

// A package over the declaration threshold and over the file threshold is a
// god package; one under threshold on either required axis is not.
func TestGodPackage_FlagsOversizedPackage(t *testing.T) {
	big := map[string]string{}
	for i := 0; i < GodPackageMaxFiles+1; i++ {
		src := "package p\n"
		// Enough decls per file that the package clears GodPackageMaxDecls.
		for j := 0; j < 8; j++ {
			src += fmt.Sprintf("func F%d_%d() {}\n", i, j)
		}
		big[fmt.Sprintf("f%d.go", i)] = src
	}
	paths := writeTempPkg(t, big)
	got := DetectGodPackages(paths, "")
	if len(got) != 1 {
		t.Fatalf("expected 1 god package, got %d: %+v", len(got), got)
	}
	if got[0].Decls <= GodPackageMaxDecls || got[0].Files <= GodPackageMaxFiles {
		t.Errorf("flagged package below a required threshold: %+v", got[0])
	}
	joined := strings.Join(got[0].Reasons, "; ")
	if !strings.Contains(joined, "declarations") || !strings.Contains(joined, "files") {
		t.Errorf("reasons missing decl/file axes: %q", joined)
	}
}

// A package with many declarations but few files and no intra-module coupling
// is large-but-cohesive and must not be flagged.
func TestGodPackage_ManyDeclsFewFilesNotFlagged(t *testing.T) {
	src := "package p\n"
	for j := 0; j < GodPackageMaxDecls+50; j++ {
		src += fmt.Sprintf("func F%d() {}\n", j)
	}
	paths := writeTempPkg(t, map[string]string{"one.go": src})
	if got := DetectGodPackages(paths, ""); len(got) != 0 {
		t.Fatalf("single-file package should not be a god package, got %+v", got)
	}
}

// The coupling axis: over the decl threshold plus high intra-module fan-out is
// a god package even when the file count is modest.
func TestGodPackage_CouplingAxis(t *testing.T) {
	const mod = "example.com/m"
	src := "package p\n"
	imports := ""
	for j := 0; j < GodPackageMaxFanOut+1; j++ {
		imports += fmt.Sprintf("import dep%d %q\n", j, fmt.Sprintf("%s/dep%d", mod, j))
	}
	src += imports
	for j := 0; j < GodPackageMaxDecls+10; j++ {
		src += fmt.Sprintf("func F%d() {}\n", j)
	}
	// Reference the imports so the file compiles conceptually (not required for
	// ImportsOnly parsing, but keeps the fixture honest).
	paths := writeTempPkg(t, map[string]string{"c.go": src})
	got := DetectGodPackages(paths, mod)
	if len(got) != 1 {
		t.Fatalf("expected coupling-driven god package, got %+v", got)
	}
	if got[0].FanOut <= GodPackageMaxFanOut {
		t.Errorf("fan-out not counted: %+v", got[0])
	}
}

// Test files do not count toward the size metrics.
func TestGodPackage_IgnoresTestFiles(t *testing.T) {
	files := map[string]string{}
	for i := 0; i < GodPackageMaxFiles+5; i++ {
		src := "package p\n"
		for j := 0; j < 10; j++ {
			src += fmt.Sprintf("func TF%d_%d(t *testing.T) {}\n", i, j)
		}
		files[fmt.Sprintf("f%d_test.go", i)] = src
	}
	paths := writeTempPkg(t, files)
	if got := DetectGodPackages(paths, ""); len(got) != 0 {
		t.Fatalf("test-only files must not form a god package, got %+v", got)
	}
}
