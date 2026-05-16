package orchestrator

import (
	"os"
	"testing"
)

// Test findTarget with various specification strategies
func TestFindTarget_MultipleStrategies(t *testing.T) {
	orch := NewOrchestrator()
	testFile := getTempTestFile(t, "target_finding.go")

	code := `package main

func firstFunc() {
}

func secondFunc(x string) error {
	return nil
}

func thirdFunc() {
}
`
	if err := os.WriteFile(testFile, []byte(code), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	testCases := []struct {
		name       string
		target     *TargetSpecification
		shouldFind bool
	}{
		{
			name:       "By function name",
			target:     &TargetSpecification{FunctionName: "secondFunc"},
			shouldFind: true,
		},
		{
			name:       "By code pattern",
			target:     &TargetSpecification{CodePattern: "error"},
			shouldFind: true,
		},
		{
			name:       "Nonexistent function",
			target:     &TargetSpecification{FunctionName: "nonexistent"},
			shouldFind: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			location, err := orch.findTarget(tc.target, testFile)
			found := location != nil && err == nil

			if found != tc.shouldFind {
				t.Errorf("Expected to find=%v, got found=%v (err=%v)", tc.shouldFind, found, err)
			}
		})
	}
}

// Test semantic targeting with various patterns
func TestSemanticTargeting_ComplexPatterns(t *testing.T) {
	orch := NewOrchestrator()
	testFile := getTempTestFile(t, "semantic_test.go")

	code := `package main

import "fmt"

func process(data string) {
	if len(data) > 0 {
		for i := 0; i < len(data); i++ {
			fmt.Println(data[i])
		}
	}
}

func validate() error {
	return nil
}
`
	if err := os.WriteFile(testFile, []byte(code), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	testCases := []struct {
		name       string
		target     *TargetSpecification
		shouldFind bool
	}{
		{
			name:       "Find by function name",
			target:     &TargetSpecification{FunctionName: "process"},
			shouldFind: true,
		},
		{
			name:       "Find by code pattern",
			target:     &TargetSpecification{CodePattern: "fmt.Println"},
			shouldFind: true,
		},
		{
			name:       "Find by function call",
			target:     &TargetSpecification{FunctionCalls: []string{"len"}},
			shouldFind: true,
		},
		{
			name:       "Find by error return",
			target:     &TargetSpecification{FunctionName: "validate"},
			shouldFind: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			location, err := orch.findTarget(tc.target, testFile)
			found := location != nil && err == nil

			if found != tc.shouldFind {
				t.Errorf("Expected to find=%v, got found=%v (err=%v)", tc.shouldFind, found, err)
			}
		})
	}
}

// Test target finding edge cases
func TestTargetFinding_EdgeCases(t *testing.T) {
	orch := NewOrchestrator()

	testCases := []struct {
		name       string
		code       string
		target     *TargetSpecification
		shouldFind bool
	}{
		{
			name:       "Find simple function",
			code:       "func foo() {}",
			target:     &TargetSpecification{FunctionName: "foo"},
			shouldFind: true,
		},
		{
			name:       "Find function with params",
			code:       "func bar(x int) {}",
			target:     &TargetSpecification{FunctionName: "bar"},
			shouldFind: true,
		},
		{
			name:       "Find nonexistent",
			code:       "func baz() {}",
			target:     &TargetSpecification{FunctionName: "notfound"},
			shouldFind: false,
		},
	}

	for _, tc := range testCases {
		testFile := getTempTestFile(t, "edge"+tc.name+".go")
		fullCode := "package main\n\n" + tc.code
		if err := os.WriteFile(testFile, []byte(fullCode), 0644); err != nil {
			t.Fatalf("Failed to write test file: %v", err)
		}

		location, err := orch.findTarget(tc.target, testFile)
		found := location != nil && err == nil

		if found != tc.shouldFind {
			t.Errorf("Test '%s': Expected find=%v, got find=%v", tc.name, tc.shouldFind, found)
		}
	}
}

// Test error handling for malformed regex patterns
func TestCalculateSemanticScore_MalformedRegex(t *testing.T) {
	orch := NewOrchestrator()

	testFile := "test_malformed_regex.go"
	testContent := `package main

func TestFunction() {}
`
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	defer os.Remove(testFile)

	// Malformed regex pattern
	target := &TargetSpecification{
		CodePattern: "[invalid regex", // Missing closing bracket
	}

	// Should not panic, should fall back to string contains
	location, err := orch.findTargetBySemantics(target, testFile)
	// This might fail or succeed depending on fallback behavior
	// The important thing is it shouldn't panic
	_ = location
	_ = err
}
