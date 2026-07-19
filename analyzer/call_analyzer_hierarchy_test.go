package analyzer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCallSiteInformation(t *testing.T) {
	tmpDir := createTempTestDir(t)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	testFile := filepath.Join(tmpDir, "test.go")
	content := `package main

func Helper() {}

func Caller() {
	Helper()
}
`
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	ca := NewCallAnalyzer([]string{testFile})
	analysis, err := ca.FindCallers("Helper", "")

	if err != nil {
		t.Fatalf("FindCallers error: %v", err)
	}

	if len(analysis.DirectCallers) != 1 {
		t.Errorf("expected 1 caller, got %d", len(analysis.DirectCallers))
	}

	site := analysis.DirectCallers[0]

	// Verify call site information
	if site.CallerName != "Caller" {
		t.Errorf("expected caller name Caller, got %s", site.CallerName)
	}

	if site.Line == 0 {
		t.Errorf("expected valid line number, got 0")
	}

	if site.File != testFile {
		t.Errorf("expected file %s, got %s", testFile, site.File)
	}

	if !contains(site.Snippet, "Helper") {
		t.Errorf("expected snippet to contain Helper, got %s", site.Snippet)
	}
}

// Should have at least one caller (Level1)

func TestMultipleCallsToSameFunction(t *testing.T) {
	tmpDir := createTempTestDir(t)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	testFile := filepath.Join(tmpDir, "test.go")
	content := `package main

func Target() {}

func Caller() {
	Target()
	Target()
	Target()
}
`
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	ca := NewCallAnalyzer([]string{testFile})
	analysis, err := ca.FindCallers("Target", "")

	if err != nil {
		t.Fatalf("FindCallers error: %v", err)
	}

	// Should find 3 call sites (multiple calls from same function)
	if len(analysis.DirectCallers) != 3 {
		t.Errorf("expected 3 call sites, got %d", len(analysis.DirectCallers))
	}

	// All should be from same caller
	for _, caller := range analysis.DirectCallers {
		if caller.CallerName != "Caller" {
			t.Errorf("expected caller Caller, got %s", caller.CallerName)
		}
	}
}

func TestExportedStatus(t *testing.T) {
	tmpDir := createTempTestDir(t)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	testFile := filepath.Join(tmpDir, "test.go")
	content := `package main

func ExportedFunc() {}

func unexportedFunc() {}

func Caller() {
	ExportedFunc()
	unexportedFunc()
}
`
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	ca := NewCallAnalyzer([]string{testFile})

	// Check exported function
	exported, _ := ca.FindCallers("ExportedFunc", "")
	if !exported.IsExported {
		t.Errorf("ExportedFunc should be marked as exported")
	}

	// Check unexported function
	unexported, _ := ca.FindCallers("unexportedFunc", "")
	if unexported.IsExported {
		t.Errorf("unexportedFunc should not be marked as exported")
	}
}
