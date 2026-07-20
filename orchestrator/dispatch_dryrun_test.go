package orchestrator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ─── isCrossPackageMove ────────────────────────────────────────────────────

func TestIsCrossPackageMove_SameDir(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.go")
	b := filepath.Join(dir, "b.go")
	src := "package mypkg\nfunc A() {}\n"
	if err := os.WriteFile(a, []byte(src), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte(src), 0600); err != nil {
		t.Fatal(err)
	}
	if isCrossPackageMove(a, b) {
		t.Error("files in the same directory/package should not be cross-package")
	}
}

func TestIsCrossPackageMove_DifferentPkg(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()
	src := filepath.Join(srcDir, "src.go")
	dst := filepath.Join(dstDir, "dst.go")
	if err := os.WriteFile(src, []byte("package alpha\nfunc F() {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("package beta\nfunc G() {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if !isCrossPackageMove(src, dst) {
		t.Error("files in different packages should be cross-package")
	}
}

func TestIsCrossPackageMove_DifferentDirSamePkg(t *testing.T) {
	// Unusual but valid: two directories that both declare the same package name.
	// isCrossPackageMove should return false because the package name matches.
	srcDir := t.TempDir()
	dstDir := t.TempDir()
	src := filepath.Join(srcDir, "src.go")
	dst := filepath.Join(dstDir, "dst.go")
	if err := os.WriteFile(src, []byte("package mypkg\nfunc F() {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("package mypkg\nfunc G() {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if isCrossPackageMove(src, dst) {
		t.Error("different dirs but same package name should not be treated as cross-package")
	}
}

// ─── dispatchOperation cross-package routing ──────────────────────────────

// TestDispatchRoutesToCrossPackage verifies that dispatchOperation routes a
// move_method to executeCrossPackageMove when the destination is in a
// different package, and that the handler's error bubbles up correctly.
func TestDispatchRoutesToCrossPackage_ErrorPropagates(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	srcFile := filepath.Join(srcDir, "src.go")
	dstFile := filepath.Join(dstDir, "nonexistent_dest.go")

	if err := os.WriteFile(srcFile, []byte("package alpha\nfunc MyFunc() {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// Write a destination package file so detectDestPackageName works.
	if err := os.WriteFile(filepath.Join(dstDir, "existing.go"), []byte("package beta\n"), 0600); err != nil {
		t.Fatal(err)
	}

	orch := NewOrchestrator()
	op := &RefactoringOperation{
		Type: "move_method",
		File: srcFile,
		Target: &TargetSpecification{
			FunctionName: "MyFunc",
		},
		Parameters: map[string]interface{}{
			"newFile": dstFile,
		},
	}
	res, err := orch.ExecuteOperations([]*RefactoringOperation{op})
	if err != nil {
		t.Fatalf("ExecuteOperations returned unexpected hard error: %v", err)
	}
	// The cross-package handler requires a go.mod; without one it should
	// fail loudly (not silently succeed).
	if res.Success {
		t.Error("expected failure when no go.mod is present for cross-package move, got success")
	}
	if len(res.Errors) == 0 {
		t.Error("expected a non-empty error list on cross-package move failure")
	}
}

// ─── simulateOperationChange (dry-run) ───────────────────────────────────

func TestSimulateOperationChange_RenameDecl(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.go")
	// rename_declaration only supports unexported identifiers.
	code := `package mypkg

func oldName() {}
`
	if err := os.WriteFile(src, []byte(code), 0600); err != nil {
		t.Fatal(err)
	}

	orch := NewOrchestrator()
	op := &RefactoringOperation{
		Type: "rename_declaration",
		File: src,
		Target: &TargetSpecification{
			FunctionName: "oldName",
		},
		Parameters: map[string]interface{}{
			"newName": "newNameRenamed",
		},
	}

	diffs, err := orch.simulateOperationChange(op)
	if err != nil {
		t.Fatalf("simulateOperationChange returned error: %v", err)
	}

	// Verify the original file was not modified.
	after, _ := os.ReadFile(src)
	if !strings.Contains(string(after), "oldName") {
		t.Error("dry-run must not modify the original file")
	}

	// Verify that a diff was produced showing the rename.
	if len(diffs) == 0 {
		t.Fatal("expected at least one diff from rename_declaration dry-run")
	}
	found := false
	for _, d := range diffs {
		if strings.Contains(d.NewCode, "newNameRenamed") {
			found = true
		}
	}
	if !found {
		t.Error("expected diff to contain new name 'newNameRenamed'")
	}
}

func TestSimulateOperationChange_NoChanges(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.go")
	code := "package mypkg\n"
	if err := os.WriteFile(src, []byte(code), 0600); err != nil {
		t.Fatal(err)
	}

	orch := NewOrchestrator()
	// An insert_code with no actual content change results in no diffs.
	op := &RefactoringOperation{
		Type: "rename_declaration",
		File: src,
		Target: &TargetSpecification{
			FunctionName: "DoesNotExist",
		},
		Parameters: map[string]interface{}{
			"newName": "AlsoDoesNotExist",
		},
	}

	// Rename of a non-existent symbol: operation fails or produces no diffs.
	// Either is acceptable — just verify original file is untouched.
	_, _ = orch.simulateOperationChange(op)
	after, _ := os.ReadFile(src)
	if string(after) != code {
		t.Error("dry-run must not modify the original file even when operation fails")
	}
}

// ─── ExecutePlanDryRun end-to-end ─────────────────────────────────────────

func TestExecutePlanDryRun_ProducesFileDiffs(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.go")
	// rename_declaration only supports unexported identifiers.
	code := `package mypkg

func oldName() {}
`
	if err := os.WriteFile(src, []byte(code), 0600); err != nil {
		t.Fatal(err)
	}

	orch := NewOrchestrator()
	plan := &RefactoringPlan{
		Version: "1.0",
		Name:    "test-dryrun",
		Operations: []*RefactoringOperation{
			{
				Type: "rename_declaration",
				File: src,
				Target: &TargetSpecification{
					FunctionName: "oldName",
				},
				Parameters: map[string]interface{}{
					"newName": "newNameRenamed",
				},
			},
		},
	}
	if err := orch.RegisterPlan(plan); err != nil {
		t.Fatalf("RegisterPlan: %v", err)
	}

	result, err := orch.ExecutePlanDryRun("test-dryrun")
	if err != nil {
		t.Fatalf("ExecutePlanDryRun: %v", err)
	}
	if len(result.Operations) == 0 {
		t.Fatal("expected at least one operation result")
	}

	// Original file must be untouched.
	after, _ := os.ReadFile(src)
	if !strings.Contains(string(after), "oldName") {
		t.Error("ExecutePlanDryRun must not modify the original file")
	}

	// Summary should mention the plan name.
	if !strings.Contains(result.Summary, "test-dryrun") {
		t.Errorf("summary does not mention plan name: %q", result.Summary)
	}
}
