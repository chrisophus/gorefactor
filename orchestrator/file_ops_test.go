package orchestrator

import (
	"os"
	"testing"
	"time"
)

// Test create_file operation
func TestExecuteCreateFile_NewFile(t *testing.T) {
	orch := NewOrchestrator()

	testFile := "test_create_file.go"
	defer os.Remove(testFile)

	codeSnippet := `package main

import "fmt"

func main() {
	fmt.Println("Created by create_file")
}
`

	operation := &RefactoringOperation{
		Type:        "create_file",
		Description: "Create a new file",
		File:        testFile,
		Parameters: map[string]interface{}{
			"codeSnippet": codeSnippet,
		},
	}

	result := orch.executeOperation(operation)
	if !result.Success {
		t.Fatalf("executeOperation() failed: %s", result.Error)
	}

	if len(result.Changes) != 1 {
		t.Fatalf("Expected 1 change, got %d", len(result.Changes))
	}

	change := result.Changes[0]
	if change.Type != "create_file" {
		t.Errorf("Expected change type 'create_file', got '%s'", change.Type)
	}

	// Verify file was created
	if _, err := os.Stat(testFile); os.IsNotExist(err) {
		t.Fatal("File was not created")
	}

	content, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read created file: %v", err)
	}

	if string(content) != codeSnippet {
		t.Error("File content does not match expected code snippet")
	}
}

// Test create_file operation with existing file and skip fallback
func TestExecuteCreateFile_ExistingFile_SkipFallback(t *testing.T) {
	orch := NewOrchestrator()

	testFile := "test_create_existing.go"
	existingContent := "package main\n\n// Existing file\n"
	if err := os.WriteFile(testFile, []byte(existingContent), 0644); err != nil {
		t.Fatalf("Failed to create existing file: %v", err)
	}
	defer os.Remove(testFile)

	operation := &RefactoringOperation{
		Type:        "create_file",
		Description: "Create a new file",
		File:        testFile,
		Parameters: map[string]interface{}{
			"codeSnippet": "package main\n\n// New content\n",
		},
		Fallback: &FallbackStrategy{
			Type:        "skip",
			Description: "Skip if file exists",
		},
	}

	result := orch.executeOperation(operation)
	if !result.Success {
		t.Fatalf("executeOperation() should succeed with skip fallback: %s", result.Error)
	}

	if result.Applied {
		t.Error("Expected Applied to be false when skipping existing file")
	}

	// Verify original content is preserved
	content, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	if string(content) != existingContent {
		t.Error("File content was modified when it should have been skipped")
	}
}

// Integration test: Test complete operation with new file creation
func TestExecutePlan_NewFileCreation(t *testing.T) {
	orch := NewOrchestrator()

	testFile := "test_integration.go"
	defer os.Remove(testFile)

	plan := &RefactoringPlan{
		Version:     "1.0",
		Name:        "test_new_file_plan",
		Description: "Test plan for new file creation",
		Created:     time.Now(),
		Operations: []*RefactoringOperation{
			{
				Type:        "insert_code",
				Description: "Create new file with code",
				File:        testFile,
				Parameters: map[string]interface{}{
					"codeSnippet": `package main

import "fmt"

func main() {
	fmt.Println("Hello from integration test")
}
`,
					"location": map[string]interface{}{
						"type": "at_beginning",
					},
				},
			},
		},
	}

	orch.plans["test_new_file_plan"] = plan

	result, err := orch.ExecutePlan("test_new_file_plan")
	if err != nil {
		t.Fatalf("ExecutePlan() failed: %v", err)
	}

	if !result.Success {
		t.Fatalf("Plan execution failed: %v", result.Errors)
	}

	if result.Statistics.SuccessfulOperations != 1 {
		t.Errorf("Expected 1 successful operation, got %d", result.Statistics.SuccessfulOperations)
	}

	// Verify file was created
	if _, err := os.Stat(testFile); os.IsNotExist(err) {
		t.Fatal("File was not created by plan execution")
	}
}
