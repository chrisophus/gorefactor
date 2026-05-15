package analyzer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindCallersDirect(t *testing.T) {
	tmpDir := createTempTestDir(t)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create test file with function and direct callers
	testFile := filepath.Join(tmpDir, "test.go")
	content := `package main

func Helper() {
	// Does nothing
}

func Function1() {
	Helper()
}

func Function2() {
	Helper()
}

func Function3() {
	// Doesn't call Helper
}

func main() {
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

	if analysis == nil {
		t.Fatal("analysis should not be nil")
	}

	// Should find 3 direct callers: Function1, Function2, main
	if len(analysis.DirectCallers) != 3 {
		t.Errorf("expected 3 direct callers, got %d", len(analysis.DirectCallers))
	}

	if analysis.TotalCallCount != 3 {
		t.Errorf("expected total count 3, got %d", analysis.TotalCallCount)
	}

	// Verify callers
	callerNames := make(map[string]bool)
	for _, caller := range analysis.DirectCallers {
		callerNames[caller.CallerName] = true
	}

	if !callerNames["Function1"] || !callerNames["Function2"] || !callerNames["main"] {
		t.Errorf("unexpected callers: %v", callerNames)
	}
}

func TestFindCallersCrossFile(t *testing.T) {
	tmpDir := createTempTestDir(t)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// File 1: Define function
	file1 := filepath.Join(tmpDir, "lib.go")
	content1 := `package main

func Process() {
	// Does something
}
`
	if err := os.WriteFile(file1, []byte(content1), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// File 2: Call from first file
	file2 := filepath.Join(tmpDir, "service1.go")
	content2 := `package main

func ServiceA() {
	Process()
}
`
	if err := os.WriteFile(file2, []byte(content2), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// File 3: Call from second file
	file3 := filepath.Join(tmpDir, "service2.go")
	content3 := `package main

func ServiceB() {
	Process()
}
`
	if err := os.WriteFile(file3, []byte(content3), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	ca := NewCallAnalyzer([]string{file1, file2, file3})
	analysis, err := ca.FindCallers("Process", "")

	if err != nil {
		t.Fatalf("FindCallers error: %v", err)
	}

	// Should find 2 callers in different files
	if len(analysis.DirectCallers) != 2 {
		t.Errorf("expected 2 direct callers, got %d", len(analysis.DirectCallers))
	}

	// Verify files are different
	files := make(map[string]bool)
	for _, caller := range analysis.DirectCallers {
		files[caller.File] = true
	}

	if len(files) != 2 {
		t.Errorf("expected callers in 2 files, got %d", len(files))
	}
}

func TestFindCallersMethodCalls(t *testing.T) {
	tmpDir := createTempTestDir(t)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	testFile := filepath.Join(tmpDir, "test.go")
	content := `package main

type Validator struct{}

func (v *Validator) Validate(s string) bool {
	return len(s) > 0
}

func CheckEmail() {
	v := &Validator{}
	v.Validate("email@test.com")
}

func CheckPhone() {
	v := &Validator{}
	v.Validate("1234567890")
}
`
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	ca := NewCallAnalyzer([]string{testFile})
	// Note: Without type inference, we search by method name only
	analysis, err := ca.FindCallers("Validate", "")

	if err != nil {
		t.Fatalf("FindCallers error: %v", err)
	}

	// Should find 2 direct method calls to any Validate method
	if len(analysis.DirectCallers) != 2 {
		t.Errorf("expected 2 direct callers, got %d", len(analysis.DirectCallers))
	}

	// All should be from CheckEmail or CheckPhone
	for _, caller := range analysis.DirectCallers {
		if caller.CallerName != "CheckEmail" && caller.CallerName != "CheckPhone" {
			t.Errorf("unexpected caller: %s", caller.CallerName)
		}
	}
}

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

func TestBuilderCallerHierarchy(t *testing.T) {
	tmpDir := createTempTestDir(t)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	testFile := filepath.Join(tmpDir, "test.go")
	content := `package main

func Base() {}

func Level1() {
	Base()
}

func Level2() {
	Level1()
}

func Entry() {
	Level2()
}
`
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	ca := NewCallAnalyzer([]string{testFile})
	hierarchy, err := ca.BuildCallerHierarchy("Base", "", 5)

	if err != nil {
		t.Fatalf("BuildCallerHierarchy error: %v", err)
	}

	if hierarchy == nil {
		t.Fatal("hierarchy should not be nil")
	}

	if hierarchy.FunctionName != "Base" {
		t.Errorf("expected Base, got %s", hierarchy.FunctionName)
	}

	// Should have at least one caller (Level1)
	if len(hierarchy.Callers) == 0 {
		t.Errorf("expected callers in hierarchy")
	}
}

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
