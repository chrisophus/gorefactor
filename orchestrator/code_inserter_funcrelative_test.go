package orchestrator

import (
	"os"
	"strings"
	"testing"
)

// Test inserting code before a function
func TestCodeInserter_InsertCode_BeforeFunction(t *testing.T) {
	inserter := NewCodeInserter()
	tmpFile := "test_code_inserter_before.go"
	defer func() { _ = os.Remove(tmpFile) }()

	// Create existing file
	existingContent := `package main

func target() {}
`
	if err := os.WriteFile(tmpFile, []byte(existingContent), 0644); err != nil {
		t.Fatalf("Failed to create existing file: %v", err)
	}

	codeSnippet := `const BeforeConst = "value"
`

	location := &InsertionLocation{
		Type:         "before_function",
		FunctionName: "target",
	}

	result, err := inserter.InsertCode(tmpFile, location, codeSnippet)
	if err != nil {
		t.Fatalf("InsertCode() failed: %v", err)
	}

	if result == nil {
		t.Fatal("InsertCode() returned nil result")
	}

	// Verify code was inserted before the function
	content, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	contentStr := string(content)
	if !strings.Contains(contentStr, "const BeforeConst") {
		t.Error("Const was not inserted")
	}

	// Find positions
	targetPos := strings.Index(contentStr, "func target()")
	beforePos := strings.Index(contentStr, "const BeforeConst")
	if beforePos > targetPos {
		t.Error("Const was not inserted before the function")
	}
}

// Test inserting code after a function
func TestCodeInserter_InsertCode_AfterFunction(t *testing.T) {
	inserter := NewCodeInserter()
	tmpFile := "test_code_inserter_after.go"
	defer func() { _ = os.Remove(tmpFile) }()

	// Create existing file
	existingContent := `package main

func target() {}
`
	if err := os.WriteFile(tmpFile, []byte(existingContent), 0644); err != nil {
		t.Fatalf("Failed to create existing file: %v", err)
	}

	codeSnippet := `func after() {}
`

	location := &InsertionLocation{
		Type:         "after_function",
		FunctionName: "target",
	}

	result, err := inserter.InsertCode(tmpFile, location, codeSnippet)
	if err != nil {
		t.Fatalf("InsertCode() failed: %v", err)
	}

	if result == nil {
		t.Fatal("InsertCode() returned nil result")
	}

	// Verify code was inserted after the function
	content, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	contentStr := string(content)
	if !strings.Contains(contentStr, "func after()") {
		t.Error("Function was not inserted")
	}

	// Find positions
	targetPos := strings.Index(contentStr, "func target()")
	afterPos := strings.Index(contentStr, "func after()")
	if afterPos < targetPos {
		t.Error("Function was not inserted after the target function")
	}
}

// Test error handling for non-existent function
func TestCodeInserter_InsertCode_NonExistentFunction(t *testing.T) {
	inserter := NewCodeInserter()
	tmpFile := "test_code_inserter_nonexistent.go"
	defer func() { _ = os.Remove(tmpFile) }()

	// Create existing file
	existingContent := `package main

func existing() {}
`
	if err := os.WriteFile(tmpFile, []byte(existingContent), 0644); err != nil {
		t.Fatalf("Failed to create existing file: %v", err)
	}

	codeSnippet := `const TestConst = "value"
`

	location := &InsertionLocation{
		Type:         "before_function",
		FunctionName: "nonexistent",
	}

	_, err := inserter.InsertCode(tmpFile, location, codeSnippet)
	if err == nil {
		t.Error("Expected error for non-existent function, got nil")
	}

	if !strings.Contains(err.Error(), "target function not found") {
		t.Errorf("Expected 'target function not found' error, got: %v", err)
	}
}
