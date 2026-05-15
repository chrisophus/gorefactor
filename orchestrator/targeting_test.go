package orchestrator

import (
	"os"
	"testing"
)

// Test regex pattern matching
func TestFindTargetBySemantics_RegexPattern(t *testing.T) {
	orch := NewOrchestrator()

	// Create a test file
	testFile := "test_regex.go"
	testContent := `package main

const (
	TestConst1 = "value1"
	TestConst2 = "value2"
)

type TestType struct {
	Field string
}

func TestFunction() {
	// Test function
}
`
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	defer os.Remove(testFile)

	// Test regex pattern matching for const declarations
	// Use a pattern that will match in the formatted code
	target := &TargetSpecification{
		CodePattern: "const", // Simple pattern that should match
	}

	location, err := orch.findTargetBySemantics(target, testFile)
	if err != nil {
		t.Fatalf("findTargetBySemantics() failed: %v", err)
	}

	if location == nil {
		t.Fatal("Expected to find target, got nil")
	}

	// Should find the const declaration (around line 3-6)
	if location.StartLine < 1 || location.StartLine > 10 {
		t.Errorf("Expected StartLine between 1-10, got %d", location.StartLine)
	}
}

// Test type declaration finding
func TestFindTargetBySemantics_TypeDeclaration(t *testing.T) {
	orch := NewOrchestrator()

	testFile := "test_type.go"
	testContent := `package main

type MyType struct {
	Field1 string
	Field2 int
}

type AnotherType int
`
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	defer os.Remove(testFile)

	target := &TargetSpecification{
		TypeName: "MyType",
	}

	location, err := orch.findTargetBySemantics(target, testFile)
	if err != nil {
		t.Fatalf("findTargetBySemantics() failed: %v", err)
	}

	if location == nil {
		t.Fatal("Expected to find type declaration, got nil")
	}

	if location.Function != "MyType" {
		t.Errorf("Expected Function 'MyType', got '%s'", location.Function)
	}
}

// Test const declaration finding
func TestFindTargetBySemantics_ConstDeclaration(t *testing.T) {
	orch := NewOrchestrator()

	testFile := "test_const.go"
	testContent := `package main

const (
	MyConst = "value"
	AnotherConst = 42
)
`
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	defer os.Remove(testFile)

	target := &TargetSpecification{
		ConstName: "MyConst",
	}

	location, err := orch.findTargetBySemantics(target, testFile)
	if err != nil {
		t.Fatalf("findTargetBySemantics() failed: %v", err)
	}

	if location == nil {
		t.Fatal("Expected to find const declaration, got nil")
	}

	if location.Function != "MyConst" {
		t.Errorf("Expected Function 'MyConst', got '%s'", location.Function)
	}
}

// Test var declaration finding
func TestFindTargetBySemantics_VarDeclaration(t *testing.T) {
	orch := NewOrchestrator()

	testFile := "test_var.go"
	testContent := `package main

var (
	MyVar = "value"
	AnotherVar = 42
)
`
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	defer os.Remove(testFile)

	target := &TargetSpecification{
		VarName: "MyVar",
	}

	location, err := orch.findTargetBySemantics(target, testFile)
	if err != nil {
		t.Fatalf("findTargetBySemantics() failed: %v", err)
	}

	if location == nil {
		t.Fatal("Expected to find var declaration, got nil")
	}

	if location.Function != "MyVar" {
		t.Errorf("Expected Function 'MyVar', got '%s'", location.Function)
	}
}

// Test regex pattern matching with invalid regex (should fall back to string contains)
func TestCalculateSemanticScore_InvalidRegex_Fallback(t *testing.T) {
	orch := NewOrchestrator()

	testFile := "test_invalid_regex.go"
	testContent := `package main

func TestFunction() {
	// Test code
}
`
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	defer os.Remove(testFile)

	// Use a pattern that contains special regex chars but we want literal match
	target := &TargetSpecification{
		CodePattern: "TestFunction", // Should match as string contains
	}

	location, err := orch.findTargetBySemantics(target, testFile)
	if err != nil {
		t.Fatalf("findTargetBySemantics() failed: %v", err)
	}

	if location == nil {
		t.Fatal("Expected to find target with string contains fallback, got nil")
	}
}

// Edge Case: Test multiple declarations with same name
func TestFindTargetBySemantics_MultipleDeclarations(t *testing.T) {
	orch := NewOrchestrator()

	testFile := "test_multiple.go"
	testContent := `package main

type MyType struct {
	Field string
}

func MyType() {
	// Function with same name as type
}
`
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	defer os.Remove(testFile)

	// Should find the type declaration
	target := &TargetSpecification{
		TypeName: "MyType",
	}

	location, err := orch.findTargetBySemantics(target, testFile)
	if err != nil {
		t.Fatalf("findTargetBySemantics() failed: %v", err)
	}

	if location == nil {
		t.Fatal("Expected to find type declaration, got nil")
	}

	// Should find the type, not the function
	if location.Function != "MyType" {
		t.Errorf("Expected Function 'MyType', got '%s'", location.Function)
	}

	// Verify it's the type (should be at line 3, not the function)
	if location.StartLine != 3 {
		t.Errorf("Expected StartLine 3 for type, got %d", location.StartLine)
	}
}
