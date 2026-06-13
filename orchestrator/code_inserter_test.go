package orchestrator

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// Test file creation with insert_code and at_beginning
func TestCodeInserter_InsertCode_NewFile_AtBeginning(t *testing.T) {
	inserter := NewCodeInserter()
	tmpFile := "test_code_inserter_new.go"
	defer func() { _ = os.Remove(tmpFile) }()

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
	defer func() { _ = os.Remove(tmpFile) }()

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

// Test error handling for invalid location type
func TestCodeInserter_InsertCode_InvalidLocationType(t *testing.T) {
	inserter := NewCodeInserter()
	tmpFile := "test_code_inserter_invalid.go"
	defer func() { _ = os.Remove(tmpFile) }()

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
	defer func() { _ = os.Remove(tmpFile) }()

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

func TestCodeInserter_InsertCode_AtEnd_ReportsRealFileLines(t *testing.T) {
	inserter := NewCodeInserter()
	tmpFile := "test_atend_lines_regression.go"
	defer func() { _ = os.Remove(tmpFile) }()

	var b strings.Builder
	b.WriteString("package main\n\n")
	for i := 0; i < 50; i++ {
		fmt.Fprintf(&b, "var line%d = %d\n", i, i)
	}
	existing := b.String()
	if err := os.WriteFile(tmpFile, []byte(existing), 0644); err != nil {
		t.Fatalf("write existing: %v", err)
	}
	origLines := strings.Count(existing, "\n")

	snippet := "func appended() {}\n"
	result, err := inserter.InsertCode(tmpFile, &InsertionLocation{Type: "at_end"}, snippet)
	if err != nil {
		t.Fatalf("InsertCode: %v", err)
	}
	if result.StartLine < origLines-5 {
		t.Errorf("StartLine=%d but original file had %d lines — line numbers fell out of the snippet's synthetic positions", result.StartLine, origLines)
	}
	if result.EndLine < result.StartLine {
		t.Errorf("EndLine=%d < StartLine=%d", result.EndLine, result.StartLine)
	}
}
