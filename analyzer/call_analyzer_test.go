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
