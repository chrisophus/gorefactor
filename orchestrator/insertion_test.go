package orchestrator

import (
	"os"
	"strings"
	"testing"
)

// Test file creation with insert_code and at_beginning
func TestInsertCode_FileCreation_AtBeginning(t *testing.T) {
	inserter := NewCodeInserter()
	tmpFile := "test_new_file.go"
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

// Test insert_code with at_beginning on new file (skip target finding)
func TestExecuteInsertCode_NewFile_AtBeginning(t *testing.T) {
	orch := NewOrchestrator()

	testFile := "test_insert_new.go"
	defer os.Remove(testFile)

	codeSnippet := `package main

const TestConst = "value"
`

	operation := &RefactoringOperation{
		Type:        "insert_code",
		Description: "Insert code at beginning of new file",
		File:        testFile,
		Parameters: map[string]interface{}{
			"codeSnippet": codeSnippet,
			"location": map[string]interface{}{
				"type": "at_beginning",
			},
		},
	}

	result := orch.executeOperation(operation)
	if !result.Success {
		t.Fatalf("executeOperation() failed: %s", result.Error)
	}

	// Verify file was created
	if _, err := os.Stat(testFile); os.IsNotExist(err) {
		t.Fatal("File was not created")
	}
}

// Edge Case: Test empty file
func TestInsertCode_EmptyFile(t *testing.T) {
	inserter := NewCodeInserter()
	testFile := "test_empty.go"
	defer os.Remove(testFile)

	// Create empty file
	if err := os.WriteFile(testFile, []byte(""), 0644); err != nil {
		t.Fatalf("Failed to create empty file: %v", err)
	}

	codeSnippet := `package main

func Test() {}
`

	location := &InsertionLocation{
		Type: "at_end",
	}

	// This should fail because empty file can't be parsed
	_, err := inserter.InsertCode(testFile, location, codeSnippet)
	if err == nil {
		t.Error("Expected error for empty file, got nil")
	}
}

// Edge Case: Test file with only comments
func TestInsertCode_CommentsOnly(t *testing.T) {
	inserter := NewCodeInserter()
	testFile := "test_comments.go"
	defer os.Remove(testFile)

	testContent := `// This is a comment
// Another comment
`
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	codeSnippet := `package main

func Test() {}
`

	location := &InsertionLocation{
		Type: "at_end",
	}

	// This should fail because file without package declaration can't be parsed
	_, err := inserter.InsertCode(testFile, location, codeSnippet)
	if err == nil {
		t.Error("Expected error for file with only comments, got nil")
	}
}

// Edge Case: Test file with package declaration only
func TestInsertCode_PackageOnly(t *testing.T) {
	inserter := NewCodeInserter()
	testFile := "test_package_only.go"
	defer os.Remove(testFile)

	testContent := `package main
`
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	codeSnippet := `const TestConst = "value"
`

	location := &InsertionLocation{
		Type: "at_end",
	}

	result, err := inserter.InsertCode(testFile, location, codeSnippet)
	if err != nil {
		t.Fatalf("InsertCode() failed: %v", err)
	}

	if result == nil {
		t.Fatal("InsertCode() returned nil result")
	}

	// Verify code was inserted
	content, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	if !strings.Contains(string(content), "const TestConst") {
		t.Error("Code was not inserted into package-only file")
	}
}
