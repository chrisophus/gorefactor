package orchestrator

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestExecutePlan_ValidPlan(t *testing.T) {
	orch := NewOrchestrator()
	testFile := getTempTestFile(t, "valid_plan.go")

	plan := &RefactoringPlan{
		Version:     "1.0",
		Name:        "test_plan",
		Description: "Test plan",
		Created:     time.Now(),
		Author:      "Test",
		Operations: []*RefactoringOperation{
			{
				Type:        "insert_code",
				Description: "Test operation",
				File:        testFile,
				Target: &TargetSpecification{
					StartLine: &[]int{10}[0],
				},
				Parameters: map[string]interface{}{
					"codeSnippet": "func test() {}",
					"location": map[string]interface{}{
						"type": "at_end",
					},
				},
				Fallback: &FallbackStrategy{
					Type:        "skip",
					Description: "Skip if target not found",
				},
			},
		},
	}

	orch.plans["test_plan"] = plan

	result, err := orch.ExecutePlan("test_plan")
	if err != nil {
		t.Fatalf("ExecutePlan() failed: %v", err)
	}

	if result == nil {
		t.Fatal("Execution result is nil")
	}

	if result.PlanName != "test_plan" {
		t.Errorf("Expected plan name 'test_plan', got '%s'", result.PlanName)
	}

	if result.Statistics == nil {
		t.Fatal("Statistics is nil")
	}

	if result.Statistics.TotalOperations != 1 {
		t.Errorf("Expected 1 total operation, got %d", result.Statistics.TotalOperations)
	}
}

func TestExecutePlan_PlanNotFound(t *testing.T) {
	orch := NewOrchestrator()
	_, err := orch.ExecutePlan("nonexistent_plan")
	if err == nil {
		t.Fatal("Expected error for nonexistent plan, got nil")
	}
}

// Integration test: Test create_file operation in plan
func TestExecutePlan_CreateFileOperation(t *testing.T) {
	orch := NewOrchestrator()

	testFile := "test_create_plan.go"
	defer os.Remove(testFile)

	plan := &RefactoringPlan{
		Version:     "1.0",
		Name:        "test_create_file_plan",
		Description: "Test plan for create_file operation",
		Created:     time.Now(),
		Operations: []*RefactoringOperation{
			{
				Type:        "create_file",
				Description: "Create new file",
				File:        testFile,
				Parameters: map[string]interface{}{
					"codeSnippet": `package main

func Test() {}
`,
				},
				Fallback: &FallbackStrategy{
					Type:        "skip",
					Description: "Skip if file exists",
				},
			},
		},
	}

	orch.plans["test_create_file_plan"] = plan

	result, err := orch.ExecutePlan("test_create_file_plan")
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

func TestLoadPlan_ValidJSON(t *testing.T) {
	// Create a temporary plan file
	testFile := getTempTestFile(t, "load_plan.json")
	plan := &RefactoringPlan{
		Version:     "1.0",
		Name:        "test_plan",
		Description: "Test plan",
		Created:     time.Now(),
		Author:      "Test",
		Operations: []*RefactoringOperation{
			{
				Type:        "insert_code",
				Description: "Test operation",
				File:        testFile,
				Target:      &TargetSpecification{},
				Parameters:  map[string]interface{}{},
			},
		},
	}

	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("Failed to marshal plan: %v", err)
	}

	if err := os.WriteFile(testFile, data, 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	orch := NewOrchestrator()
	loadedPlan, err := orch.LoadPlan(testFile)
	if err != nil {
		t.Fatalf("LoadPlan() failed: %v", err)
	}

	if loadedPlan == nil {
		t.Fatal("Loaded plan is nil")
	}

	if loadedPlan.Name != "test_plan" {
		t.Errorf("Expected plan name 'test_plan', got '%s'", loadedPlan.Name)
	}

	if loadedPlan.Description != "Test plan" {
		t.Errorf("Expected description 'Test plan', got '%s'", loadedPlan.Description)
	}
}

func TestLoadPlan_InvalidJSON(t *testing.T) {
	// Create a temporary file with invalid JSON
	tmpFile := "invalid_plan.json"
	if err := os.WriteFile(tmpFile, []byte("invalid json"), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile) }()

	orch := NewOrchestrator()
	_, err := orch.LoadPlan(tmpFile)
	if err == nil {
		t.Fatal("Expected error for invalid JSON, got nil")
	}
}

func TestLoadPlan_FileNotFound(t *testing.T) {
	orch := NewOrchestrator()
	_, err := orch.LoadPlan("nonexistent_file.json")
	if err == nil {
		t.Fatal("Expected error for nonexistent file, got nil")
	}
}
