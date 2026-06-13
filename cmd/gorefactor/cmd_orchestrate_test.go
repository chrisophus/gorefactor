package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestOrchestrateTestFlag_PassingTests verifies that --test succeeds when the
// tests in the affected module still pass after plan execution.
func TestOrchestrateTestFlag_PassingTests(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}

	modDir := t.TempDir()

	// Set up a minimal Go module.
	gomod := `module example.com/testmod

go 1.21
`
	if err := os.WriteFile(filepath.Join(modDir, "go.mod"), []byte(gomod), 0644); err != nil {
		t.Fatal(err)
	}

	// A source file with an unexported function.
	srcFile := filepath.Join(modDir, "foo.go")
	if err := os.WriteFile(srcFile, []byte("package testmod\n\nfunc helper() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// A passing test file.
	testFile := filepath.Join(modDir, "foo_test.go")
	if err := os.WriteFile(testFile, []byte("package testmod\nimport \"testing\"\nfunc TestAlwaysPass(t *testing.T) {}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// A plan that renames the helper function (unexported → unexported).
	plan := map[string]interface{}{
		"version": "1.0",
		"name":    "test-flag-pass",
		"operations": []map[string]interface{}{
			{
				"type": "rename_declaration",
				"file": srcFile,
				"target": map[string]interface{}{
					"functionName": "helper",
				},
				"parameters": map[string]interface{}{
					"newName": "helperRenamed",
				},
			},
		},
	}
	planData, _ := json.Marshal(plan)
	planFile := filepath.Join(modDir, "plan.json")
	if err := os.WriteFile(planFile, planData, 0644); err != nil {
		t.Fatal(err)
	}

	// Run orchestrateRefactoring with --test from within the module directory.
	origDir, _ := os.Getwd()
	if err := os.Chdir(modDir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	err := orchestrateRefactoring([]string{planFile, "--test"})
	if err != nil {
		t.Fatalf("expected success with --test on passing tests, got: %v", err)
	}

	// Verify the rename was applied (not rolled back).
	content, _ := os.ReadFile(srcFile)
	if !strings.Contains(string(content), "helperRenamed") {
		t.Error("file should contain renamed function after successful --test run")
	}
}

// TestOrchestrateTestFlag_FailingTests verifies that --test restores the
// snapshot and exits with code 4 when tests fail after plan execution.
//
// Strategy: the test file contains a compile-time assertion
// (var _ = helpMe) that depends on helpMe existing. The plan deletes helpMe,
// which makes compilation fail, causing `go test` to exit non-zero.
func TestOrchestrateTestFlag_FailingTests(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}

	modDir := t.TempDir()

	// Set up a minimal Go module.
	gomod := `module example.com/testmod2

go 1.21
`
	if err := os.WriteFile(filepath.Join(modDir, "go.mod"), []byte(gomod), 0644); err != nil {
		t.Fatal(err)
	}

	// A source file with a function that our test references directly at
	// the package level — deleting it will break compilation.
	srcFile := filepath.Join(modDir, "bar.go")
	srcCode := `package testmod2

func helpMeFunc() string { return "ok" }

func anotherFunc() {}
`
	if err := os.WriteFile(srcFile, []byte(srcCode), 0644); err != nil {
		t.Fatal(err)
	}

	// A test that references helpMeFunc by value — if it's deleted the
	// package will not compile and `go test` will exit non-zero.
	testFile := filepath.Join(modDir, "bar_test.go")
	testCode := `package testmod2

import "testing"

// compile-time reference: if helpMeFunc is deleted this file won't compile.
var _ = helpMeFunc

func TestAlwaysPass(t *testing.T) {}
`
	if err := os.WriteFile(testFile, []byte(testCode), 0644); err != nil {
		t.Fatal(err)
	}

	// A plan that deletes helpMeFunc, which breaks the test file's compile.
	plan := map[string]interface{}{
		"version": "1.0",
		"name":    "test-flag-fail",
		"operations": []map[string]interface{}{
			{
				"type": "delete_declaration",
				"file": srcFile,
				"target": map[string]interface{}{
					"functionName": "helpMeFunc",
				},
			},
		},
	}
	planData, _ := json.Marshal(plan)
	planFile := filepath.Join(modDir, "plan.json")
	if err := os.WriteFile(planFile, planData, 0644); err != nil {
		t.Fatal(err)
	}

	origDir, _ := os.Getwd()
	if err := os.Chdir(modDir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	err := orchestrateRefactoring([]string{planFile, "--test"})
	if err == nil {
		t.Fatal("expected gate failure error when tests fail after deletion, got nil")
	}
	if exitCodeFor(err) != exitGateFailure {
		t.Errorf("expected exit code %d (gate failure), got %d: %v", exitGateFailure, exitCodeFor(err), err)
	}

	// Snapshot should have been restored — original function is back.
	content, _ := os.ReadFile(srcFile)
	if !strings.Contains(string(content), "helpMeFunc") {
		t.Errorf("snapshot restore failed: expected original function 'helpMeFunc' in file, got:\n%s", content)
	}
}
