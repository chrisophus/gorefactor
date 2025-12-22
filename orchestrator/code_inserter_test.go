package orchestrator

import (
	"os"
	"strings"
	"testing"
)

// Test file creation with insert_code and at_beginning
func TestCodeInserter_InsertCode_NewFile_AtBeginning(t *testing.T) {
	inserter := NewCodeInserter()
	tmpFile := "test_code_inserter_new.go"
	defer os.Remove(tmpFile)

	codeSnippet := `package main

import "fmt"

const TestConst = "test"

func main() {
	fmt.Println("Hello")
}
`

	location := &InsertionLocation{
		Type: "at_beginning",
	}

	result, err := inserter.InsertCode(tmpFile, location, codeSnippet)
	if err != nil {
		t.Fatalf("InsertCode() failed: %v", err)
	}

	if result == nil {
		t.Fatal("InsertCode() returned nil result")
	}

	if result.StartLine != 1 {
		t.Errorf("Expected StartLine 1, got %d", result.StartLine)
	}

	// Verify file was created
	if _, err := os.Stat(tmpFile); os.IsNotExist(err) {
		t.Fatal("File was not created")
	}

	// Verify file contents
	content, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("Failed to read created file: %v", err)
	}

	if !strings.Contains(string(content), "package main") {
		t.Error("File content does not contain package declaration")
	}

	if !strings.Contains(string(content), "const TestConst") {
		t.Error("File content does not contain const declaration")
	}
}

// Test inserting code into existing file
func TestCodeInserter_InsertCode_ExistingFile_AtEnd(t *testing.T) {
	inserter := NewCodeInserter()
	tmpFile := "test_code_inserter_existing.go"
	defer os.Remove(tmpFile)

	// Create existing file
	existingContent := `package main

func existing() {}
`
	if err := os.WriteFile(tmpFile, []byte(existingContent), 0644); err != nil {
		t.Fatalf("Failed to create existing file: %v", err)
	}

	codeSnippet := `func newFunction() {}
`

	location := &InsertionLocation{
		Type: "at_end",
	}

	result, err := inserter.InsertCode(tmpFile, location, codeSnippet)
	if err != nil {
		t.Fatalf("InsertCode() failed: %v", err)
	}

	if result == nil {
		t.Fatal("InsertCode() returned nil result")
	}

	// Verify code was inserted
	content, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	if !strings.Contains(string(content), "func newFunction()") {
		t.Error("New function was not inserted")
	}

	if !strings.Contains(string(content), "func existing()") {
		t.Error("Existing function was removed")
	}
}

// Test inserting code at beginning of existing file
func TestCodeInserter_InsertCode_ExistingFile_AtBeginning(t *testing.T) {
	inserter := NewCodeInserter()
	tmpFile := "test_code_inserter_beginning.go"
	defer os.Remove(tmpFile)

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
		Type: "at_beginning",
	}

	result, err := inserter.InsertCode(tmpFile, location, codeSnippet)
	if err != nil {
		t.Fatalf("InsertCode() failed: %v", err)
	}

	if result == nil {
		t.Fatal("InsertCode() returned nil result")
	}

	// Verify code was inserted
	content, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	contentStr := string(content)
	if !strings.Contains(contentStr, "const TestConst") {
		t.Error("Const was not inserted")
	}

	if !strings.Contains(contentStr, "func existing()") {
		t.Error("Existing function was removed")
	}
}

// Test inserting code before a function
func TestCodeInserter_InsertCode_BeforeFunction(t *testing.T) {
	inserter := NewCodeInserter()
	tmpFile := "test_code_inserter_before.go"
	defer os.Remove(tmpFile)

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
	defer os.Remove(tmpFile)

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
	defer os.Remove(tmpFile)

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

// Test error handling for invalid location type
func TestCodeInserter_InsertCode_InvalidLocationType(t *testing.T) {
	inserter := NewCodeInserter()
	tmpFile := "test_code_inserter_invalid.go"
	defer os.Remove(tmpFile)

	codeSnippet := `package main
`

	location := &InsertionLocation{
		Type: "invalid_type",
	}

	_, err := inserter.InsertCode(tmpFile, location, codeSnippet)
	if err == nil {
		t.Error("Expected error for invalid location type, got nil")
	}

	if !strings.Contains(err.Error(), "unknown insertion location type") {
		t.Errorf("Expected 'unknown insertion location type' error, got: %v", err)
	}
}

// Test package name extraction from code snippet
func TestCodeInserter_InsertCode_PackageNameExtraction(t *testing.T) {
	inserter := NewCodeInserter()
	tmpFile := "test_code_inserter_package.go"
	defer os.Remove(tmpFile)

	codeSnippet := `package testpkg

import "fmt"

func main() {
	fmt.Println("Hello")
}
`

	location := &InsertionLocation{
		Type: "at_beginning",
	}

	_, err := inserter.InsertCode(tmpFile, location, codeSnippet)
	if err != nil {
		t.Fatalf("InsertCode() failed: %v", err)
	}

	// Verify file was created with correct package name
	content, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("Failed to read created file: %v", err)
	}

	if !strings.Contains(string(content), "package testpkg") {
		t.Error("File content does not contain correct package name")
	}
}
