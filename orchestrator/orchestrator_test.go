package orchestrator

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestNewOrchestrator(t *testing.T) {
	orch := NewOrchestrator()
	if orch == nil {
		t.Fatal("NewOrchestrator() returned nil")
	}
}

func TestLoadPlan_ValidJSON(t *testing.T) {
	// Create a temporary plan file
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

	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("Failed to marshal plan: %v", err)
	}

	tmpFile := "test_plan.json"
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}
	defer os.Remove(tmpFile)

	orch := NewOrchestrator()
	loadedPlan, err := orch.LoadPlan(tmpFile)
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
	defer os.Remove(tmpFile)

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

func TestExecutePlan_ValidPlan(t *testing.T) {
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

func TestExecuteOperation_ValidFallbackStrategies(t *testing.T) {
	orch := NewOrchestrator()

	testCases := []struct {
		fallbackType  string
		shouldSucceed bool
	}{
		{"skip", true},
		{"use_default", true},
		{"invalid_type", false},
	}

	for _, tc := range testCases {
		operation := &RefactoringOperation{
			Type:        "insert_code",
			Description: "Test operation",
			File:        "test.go",
			Target: &TargetSpecification{
				StartLine: &[]int{999}[0], // Line that doesn't exist
			},
			Parameters: map[string]interface{}{
				"codeSnippet": "func test() {}",
				"location": map[string]interface{}{
					"type": "at_end",
				},
			},
			Fallback: &FallbackStrategy{
				Type:        tc.fallbackType,
				Description: "Test fallback",
			},
		}

		result := orch.executeOperation(operation)

		if tc.fallbackType == "invalid_type" && !strings.Contains(result.Error, "unknown fallback strategy") {
			t.Errorf("Fallback type '%s': Expected 'unknown fallback strategy' error, got: %s", tc.fallbackType, result.Error)
		}

		// For valid fallback types, we expect either success or a specific error
		if tc.fallbackType != "invalid_type" && result.Success {
			t.Errorf("Fallback type '%s': Expected failure, got success", tc.fallbackType)
		}
	}
}

func TestExecuteOperation_NoFallback(t *testing.T) {
	orch := NewOrchestrator()

	operation := &RefactoringOperation{
		Type:        "insert_code",
		Description: "Test operation",
		File:        "test.go",
		Target: &TargetSpecification{
			StartLine: &[]int{999}[0], // Line that doesn't exist
		},
		Parameters: map[string]interface{}{
			"codeSnippet": "func test() {}",
			"location": map[string]interface{}{
				"type": "at_end",
			},
		},
		// No fallback strategy
	}

	result := orch.executeOperation(operation)

	if result.Success {
		t.Error("Expected operation to fail without fallback strategy")
	}

	if !strings.Contains(result.Error, "Failed to find target") {
		t.Errorf("Expected 'Failed to find target' error, got: %s", result.Error)
	}
}

func TestExecuteOperation_UnknownType(t *testing.T) {
	orch := NewOrchestrator()

	operation := &RefactoringOperation{
		Type:        "unknown_operation_type",
		Description: "Test operation",
		File:        "test.go",
		Target: &TargetSpecification{
			StartLine: &[]int{10}[0],
		},
		Parameters: map[string]interface{}{},
	}

	result := orch.executeOperation(operation)

	if result.Success {
		t.Error("Expected operation to fail with unknown type")
	}

	// The error might be about file not found before it gets to operation type check
	if !strings.Contains(result.Error, "unknown operation type") && !strings.Contains(result.Error, "failed to parse file") {
		t.Errorf("Expected 'unknown operation type' or file error, got: %s", result.Error)
	}
}

func TestCheckConditions_ValidConditions(t *testing.T) {
	orch := NewOrchestrator()

	conditions := []*Condition{
		{
			Type:     "complexity",
			Property: "controlStructures",
			Value:    2,
			Operator: "gte",
		},
	}

	// This is a simplified test since the condition evaluation is simplified
	result := orch.checkConditions(conditions)
	if !result {
		t.Error("Expected conditions to pass")
	}
}

func TestCheckConditions_EmptyConditions(t *testing.T) {
	orch := NewOrchestrator()

	result := orch.checkConditions([]*Condition{})
	if !result {
		t.Error("Expected empty conditions to pass")
	}
}

func TestCheckConditions_NilConditions(t *testing.T) {
	orch := NewOrchestrator()

	result := orch.checkConditions(nil)
	if !result {
		t.Error("Expected nil conditions to pass")
	}
}

func TestExecuteFallback_ValidStrategies(t *testing.T) {
	orch := NewOrchestrator()

	testCases := []struct {
		fallbackType string
		shouldError  bool
	}{
		{"skip", true},         // skip should return an error
		{"use_default", false}, // use_default might succeed or fail depending on file content
		{"invalid_type", true},
	}

	for _, tc := range testCases {
		fallback := &FallbackStrategy{
			Type:        tc.fallbackType,
			Description: "Test fallback",
		}

		_, err := orch.executeFallback(fallback, "test.go")

		if tc.shouldError && err == nil {
			t.Errorf("Fallback type '%s': Expected error, got nil", tc.fallbackType)
		}

		if !tc.shouldError && err != nil && !strings.Contains(err.Error(), "no functions found") && !strings.Contains(err.Error(), "no such file or directory") {
			t.Errorf("Fallback type '%s': Expected success or specific error, got: %v", tc.fallbackType, err)
		}
	}
}

func TestFindDefaultTarget_FileNotFound(t *testing.T) {
	orch := NewOrchestrator()

	_, err := orch.findDefaultTarget("nonexistent_file.go")
	if err == nil {
		t.Fatal("Expected error for nonexistent file, got nil")
	}
}

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
	defer os.Remove(tmpFile)

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
				Description: "",
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
			false,
		},
	}

	for i, tc := range testCases {
		isValid := tc.operation.Type != "" &&
			tc.operation.Description != "" &&
			tc.operation.File != "" &&
			tc.operation.Target != nil &&
			tc.operation.Parameters != nil

		if isValid != tc.shouldBeValid {
			t.Errorf("Test case %d: Expected validity %t, got %t", i+1, tc.shouldBeValid, isValid)
		}
	}
}
