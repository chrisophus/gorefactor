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

func TestIsCallableFrom(t *testing.T) {
	tmpDir := createTempTestDir(t)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	testFile := filepath.Join(tmpDir, "test.go")
	content := `package main

func Target() {
	// Target function
}

func Caller1() {
	Target()
}

func Caller2() {
	// Doesn't call Target
}
`
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	ca := NewCallAnalyzer([]string{testFile})

	// Test that Caller1 can call Target
	canCall, err := ca.IsCallableFrom("Caller1", "", "Target", "")
	if err != nil {
		t.Fatalf("IsCallableFrom error: %v", err)
	}

	if !canCall {
		t.Errorf("expected Caller1 to call Target")
	}

	// Test that Caller2 cannot call Target
	canCall, err = ca.IsCallableFrom("Caller2", "", "Target", "")
	if err != nil {
		t.Fatalf("IsCallableFrom error: %v", err)
	}

	if canCall {
		t.Errorf("expected Caller2 not to call Target")
	}
}

func TestFindCallChainSimple(t *testing.T) {
	tmpDir := createTempTestDir(t)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	testFile := filepath.Join(tmpDir, "test.go")
	content := `package main

func A() {
	B()
}

func B() {
	C()
}

func C() {
	// Base case
}

func main() {
	A()
}
`
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	ca := NewCallAnalyzer([]string{testFile})
	// Test that we can find callers of C (which is B)
	chain, err := ca.FindCallChain("C", "", "C", "", 5)

	if err != nil {
		t.Fatalf("FindCallChain error: %v", err)
	}

	if chain == nil {
		t.Fatal("chain should not be nil")
	}

	// Should detect that C is the target
	if chain.Start != "C" {
		t.Errorf("expected start C, got %s", chain.Start)
	}
}

func TestDetectCycleInCallGraph(t *testing.T) {
	tmpDir := createTempTestDir(t)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	testFile := filepath.Join(tmpDir, "test.go")
	content := `package main

func A() {
	B()
}

func B() {
	C()
}

func C() {
	A() // Creates cycle!
}
`
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	ca := NewCallAnalyzer([]string{testFile})
	chain, err := ca.FindCallChain("A", "", "B", "", 5)

	if err != nil {
		t.Fatalf("FindCallChain error: %v", err)
	}

	// Should detect cycle
	if !chain.IsCircular {
		t.Errorf("expected to detect circular call")
	}
}
