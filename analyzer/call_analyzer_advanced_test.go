package analyzer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindCallersTestCode(t *testing.T) {
	tmpDir := createTempTestDir(t)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Production file
	prodFile := filepath.Join(tmpDir, "lib.go")
	prodContent := `package main

func DoWork() {
	// Implementation
}

func Caller1() {
	DoWork()
}
`
	if err := os.WriteFile(prodFile, []byte(prodContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Test file
	testFile := filepath.Join(tmpDir, "lib_test.go")
	testContent := `package main

import "testing"

func TestDoWork(t *testing.T) {
	DoWork()
}
`
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	ca := NewCallAnalyzer([]string{prodFile, testFile})
	analysis, err := ca.FindCallers("DoWork", "")

	if err != nil {
		t.Fatalf("FindCallers error: %v", err)
	}

	// Should find 1 direct caller and 1 test caller
	if len(analysis.DirectCallers) != 1 {
		t.Errorf("expected 1 direct caller, got %d", len(analysis.DirectCallers))
	}

	if len(analysis.TestCallers) != 1 {
		t.Errorf("expected 1 test caller, got %d", len(analysis.TestCallers))
	}

	if analysis.TestCallers[0].CallerName != "TestDoWork" {
		t.Errorf("expected test caller TestDoWork, got %s", analysis.TestCallers[0].CallerName)
	}
}

// Test that Caller1 can call Target

// Test that Caller2 cannot call Target

// Test that we can find callers of C (which is B)

// Should detect that C is the target

// Should detect cycle
