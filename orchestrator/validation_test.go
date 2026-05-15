package orchestrator

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestValidatePlan_ValidPlan(t *testing.T) {
	orch := NewOrchestrator()

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
				File:        "test.go",
				Target:      &TargetSpecification{},
				Parameters:  map[string]interface{}{},
			},
		},
	}

	err := orch.validatePlan(plan)
	if err != nil {
		t.Errorf("Expected valid plan to pass validation, got error: %v", err)
	}
}

func TestValidatePlan_InvalidPlan(t *testing.T) {
	orch := NewOrchestrator()

	// Plan with missing required fields
	plan := &RefactoringPlan{
		// Missing Version, Name, etc.
	}

	err := orch.validatePlan(plan)
	if err == nil {
		t.Error("Expected invalid plan to fail validation")
	}
}

func TestSaveResult_ValidResult(t *testing.T) {
	orch := NewOrchestrator()

	result := &ExecutionResult{
		PlanName: "test_plan",
		Executed: time.Now(),
		Success:  true,
		Statistics: &ExecutionStatistics{
			TotalOperations:      1,
			SuccessfulOperations: 1,
			FailedOperations:     0,
		},
	}

	tmpFile := "test_result.json"
	err := orch.SaveResult(result, tmpFile)
	if err != nil {
		t.Fatalf("SaveResult() failed: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile) }()

	// Verify the file was created and contains valid JSON
	data, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("Failed to read result file: %v", err)
	}

	var savedResult ExecutionResult
	if err := json.Unmarshal(data, &savedResult); err != nil {
		t.Fatalf("Failed to unmarshal saved result: %v", err)
	}

	if savedResult.PlanName != "test_plan" {
		t.Errorf("Expected plan name 'test_plan', got '%s'", savedResult.PlanName)
	}

	if !savedResult.Success {
		t.Error("Expected success to be true")
	}
}

func TestSaveResult_InvalidPath(t *testing.T) {
	orch := NewOrchestrator()

	result := &ExecutionResult{
		PlanName: "test_plan",
		Executed: time.Now(),
		Success:  true,
	}

	// Try to save to a directory that doesn't exist
	err := orch.SaveResult(result, "/nonexistent/directory/result.json")
	if err == nil {
		t.Error("Expected error for invalid path, got nil")
	}
}

func TestPlanSerialization(t *testing.T) {
	testFile := getTempTestFile(t, "serialization.go")

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
				},
				Fallback: &FallbackStrategy{
					Type:        "skip",
					Description: "Skip if target not found",
				},
			},
		},
		Metadata: map[string]interface{}{
			"tags": []string{"test"},
		},
	}

	// Test that the plan can be serialized to JSON
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("Failed to serialize plan to JSON: %v", err)
	}

	// Test that the plan can be deserialized from JSON
	var loadedPlan RefactoringPlan
	if err := json.Unmarshal(data, &loadedPlan); err != nil {
		t.Fatalf("Failed to deserialize plan from JSON: %v", err)
	}

	if loadedPlan.Name != plan.Name {
		t.Errorf("Expected name '%s', got '%s'", plan.Name, loadedPlan.Name)
	}

	if loadedPlan.Description != plan.Description {
		t.Errorf("Expected description '%s', got '%s'", plan.Description, loadedPlan.Description)
	}

	if len(loadedPlan.Operations) != len(plan.Operations) {
		t.Errorf("Expected %d operations, got %d", len(plan.Operations), len(loadedPlan.Operations))
	}
}

func TestOperationValidation(t *testing.T) {
	testCases := []struct {
		operation     *RefactoringOperation
		shouldBeValid bool
	}{
		{
			&RefactoringOperation{
				Type:        "insert_code",
				Description: "Test operation",
				File:        "test.go",
				Target:      &TargetSpecification{},
				Parameters:  map[string]interface{}{},
			},
			true,
		},
		{
			&RefactoringOperation{
				Type:        "",
				Description: "Test operation",
				File:        "test.go",
				Target:      &TargetSpecification{},
				Parameters:  map[string]interface{}{},
			},
			false,
		},
		{
			&RefactoringOperation{
				Type:        "insert_code",
				Description: "Test operation",
				File:        "",
				Target:      nil,
				Parameters:  map[string]interface{}{},
			},
			false, // File is required
		},
		{
			&RefactoringOperation{
				Type:        "insert_code",
				Description: "Test operation",
				File:        "",
				Target:      &TargetSpecification{},
				Parameters:  map[string]interface{}{},
			},
			false,
		},
		{
			&RefactoringOperation{
				Type:        "insert_code",
				Description: "Test operation",
				File:        "test.go",
				Target:      nil,
				Parameters:  map[string]interface{}{},
			},
			true, // insert_code allows nil target
		},
		{
			&RefactoringOperation{
				Type:        "create_file",
				Description: "Test operation",
				File:        "test.go",
				Target:      nil,
				Parameters:  map[string]interface{}{},
			},
			true, // create_file allows nil target
		},
	}

	orch := NewOrchestrator()
	for i, tc := range testCases {
		err := orch.validateOperation(tc.operation)
		isValid := err == nil

		if isValid != tc.shouldBeValid {
			t.Errorf("Test case %d: Expected validity %t, got %t (error: %v)", i+1, tc.shouldBeValid, isValid, err)
		}
	}
}
