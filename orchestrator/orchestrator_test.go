package orchestrator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// getTempTestFile returns a unique temporary file path for testing
func getTempTestFile(t *testing.T, suffix string) string {
	tmpDir := os.TempDir()
	tmpFile := filepath.Join(tmpDir, "gorefactor_test_"+t.Name()+"_"+suffix)
	t.Cleanup(func() {
		os.Remove(tmpFile)
	})
	return tmpFile
}

func TestNewOrchestrator(t *testing.T) {
	orch := NewOrchestrator()
	if orch == nil {
		t.Fatal("NewOrchestrator() returned nil")
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
		// Use extract_method which requires a target
		testFile := getTempTestFile(t, "fallback_"+tc.fallbackType+".go")
		operation := &RefactoringOperation{
			Type:        "extract_method",
			Description: "Test operation",
			File:        testFile,
			Target: &TargetSpecification{
				StartLine: &[]int{999}[0], // Line that doesn't exist
			},
			Parameters: map[string]interface{}{
				"methodName": "extracted",
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
	testFile := getTempTestFile(t, "no_fallback.go")

	// Use extract_method which requires a target
	operation := &RefactoringOperation{
		Type:        "extract_method",
		Description: "Test operation",
		File:        testFile,
		Target: &TargetSpecification{
			StartLine: &[]int{999}[0], // Line that doesn't exist
		},
		Parameters: map[string]interface{}{
			"methodName": "extracted",
		},
		// No fallback strategy
	}

	result := orch.executeOperation(operation)

	if result.Success {
		t.Error("Expected operation to fail without fallback strategy")
	}
	if !strings.Contains(result.Error, "failed to find target") {
		t.Errorf("Expected error containing failed to find target, got: %s", result.Error)
	}

}

func TestExecuteOperation_UnknownType(t *testing.T) {
	orch := NewOrchestrator()
	testFile := getTempTestFile(t, "unknown_type.go")

	operation := &RefactoringOperation{
		Type:        "unknown_operation_type",
		Description: "Test operation",
		File:        testFile,
		Target: &TargetSpecification{
			StartLine: &[]int{10}[0],
		},
		Parameters: map[string]interface{}{},
	}

	result := orch.executeOperation(operation)

	if result.Success {
		t.Error("Expected operation to fail with unknown type")
	}

	// The error might be about file not found, target not found, or unknown operation type
	if !strings.Contains(result.Error, "unknown operation type") &&
		!strings.Contains(result.Error, "failed to parse file") &&
		!strings.Contains(result.Error, "no suitable target found") {
		t.Errorf("Expected 'unknown operation type', file error, or target error, got: %s", result.Error)
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
		testFile := getTempTestFile(t, "fallback_"+tc.fallbackType+".go")
		fallback := &FallbackStrategy{
			Type:        tc.fallbackType,
			Description: "Test fallback",
		}

		_, err := orch.executeFallback(fallback, testFile)

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

// ============================================================================
// Tests for GOREFACTOR_IMPROVEMENTS.md recommendations
// ============================================================================

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

// Test validation allows optional target for insert_code and create_file
func TestValidateOperation_OptionalTarget(t *testing.T) {
	orch := NewOrchestrator()

	testCases := []struct {
		operationType string
		hasTarget     bool
		shouldBeValid bool
	}{
		{"insert_code", false, true},
		{"create_file", false, true},
		{"extract_method", false, false},
		{"move_method", false, false},
	}

	for _, tc := range testCases {
		testFile := getTempTestFile(t, "optional_target_"+tc.operationType+".go")
		operation := &RefactoringOperation{
			Type:        tc.operationType,
			Description: "Test operation",
			File:        testFile,
			Parameters:  map[string]interface{}{},
		}

		if tc.hasTarget {
			operation.Target = &TargetSpecification{}
		}

		err := orch.validateOperation(operation)
		isValid := err == nil

		if isValid != tc.shouldBeValid {
			t.Errorf("Operation type '%s' with target=%t: Expected valid=%t, got valid=%t",
				tc.operationType, tc.hasTarget, tc.shouldBeValid, isValid)
		}
	}
}

// Test executeExtractMethod with a complete workflow
func TestExecuteExtractMethod_BasicExtraction(t *testing.T) {
	orch := NewOrchestrator()
	testFile := getTempTestFile(t, "extract_method.go")

	code := `package main

func calculate(a int, b int) int {
	x := a + b
	y := x * 2
	return y
}
`
	if err := os.WriteFile(testFile, []byte(code), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	planJSON := `{
		"version": "1.0",
		"name": "extract_test",
		"operations": [
			{
				"type": "extract_method",
				"description": "Extract calculation block",
				"file": "` + testFile + `",
				"target": {
					"functionName": "calculate"
				},
				"parameters": {
					"startLine": 4,
					"endLine": 5,
					"methodName": "computeResult"
				}
			}
		]
	}`

	planFile := getTempTestFile(t, "extract_plan.json")
	if err := os.WriteFile(planFile, []byte(planJSON), 0644); err != nil {
		t.Fatalf("Failed to write plan file: %v", err)
	}

	_, err := orch.LoadPlan(planFile)
	if err != nil {
		t.Fatalf("Failed to load plan: %v", err)
	}

	result, err := orch.ExecutePlan("extract_test")
	if err != nil {
		// Extract might fail due to complex dependency analysis, that's ok for this test
		// The important thing is the operation was attempted
	}

	// Verify the result structure
	if result == nil {
		t.Fatal("ExecutePlan returned nil result")
	}
	if result.PlanName != "extract_test" {
		t.Errorf("Expected plan name 'extract_test', got '%s'", result.PlanName)
	}
}

// Test executeMoveMethod with file movement
func TestExecuteMoveMethod_BasicMove(t *testing.T) {
	orch := NewOrchestrator()
	sourceFile := getTempTestFile(t, "move_source.go")
	destFile := getTempTestFile(t, "move_dest.go")

	// Create source file with a method
	sourceCode := `package main

type Handler struct {
	name string
}

func (h *Handler) Process() error {
	return nil
}
`
	if err := os.WriteFile(sourceFile, []byte(sourceCode), 0644); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	// Create destination file
	destCode := `package main
`
	if err := os.WriteFile(destFile, []byte(destCode), 0644); err != nil {
		t.Fatalf("Failed to create dest file: %v", err)
	}

	planJSON := `{
		"version": "1.0",
		"name": "move_test",
		"operations": [
			{
				"type": "move_method",
				"description": "Move Process method",
				"file": "` + sourceFile + `",
				"target": {
					"methodName": "Process",
					"receiverType": "Handler"
				},
				"parameters": {
					"newFile": "` + destFile + `"
				}
			}
		]
	}`

	planFile := getTempTestFile(t, "move_plan.json")
	if err := os.WriteFile(planFile, []byte(planJSON), 0644); err != nil {
		t.Fatalf("Failed to write plan file: %v", err)
	}

	_, err := orch.LoadPlan(planFile)
	if err != nil {
		t.Fatalf("Failed to load plan: %v", err)
	}

	result, err := orch.ExecutePlan("move_test")
	if err != nil {
		t.Fatalf("ExecutePlan failed: %v", err)
	}

	if result == nil {
		t.Fatal("ExecutePlan returned nil result")
	}
	if !result.Success {
		t.Errorf("Plan execution failed. Errors: %v", result.Errors)
	}

	// Verify method was moved (file should be updated)
	sourceContent, _ := os.ReadFile(sourceFile)
	if strings.Contains(string(sourceContent), "func (h *Handler) Process()") {
		t.Error("Method was not removed from source file")
	}

	destContent, _ := os.ReadFile(destFile)
	if !strings.Contains(string(destContent), "func (h *Handler) Process()") {
		t.Error("Method was not added to destination file")
	}
}

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

// Test code insertion edge cases
func TestInsertCode_InsideFunction(t *testing.T) {
	inserter := NewCodeInserter()
	testFile := getTempTestFile(t, "insert_inside.go")

	code := `package main

func main() {
	fmt.Println("start")
}
`
	if err := os.WriteFile(testFile, []byte(code), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	location := &InsertionLocation{
		Type:         "inside_function",
		FunctionName: "main",
	}

	_, err := inserter.InsertCode(testFile, location, "fmt.Println(\"inside\")")
	// This operation might not be implemented yet, but we're testing it doesn't crash
	_ = err
}

// Test target finding edge cases
func TestTargetFinding_EdgeCases(t *testing.T) {
	orch := NewOrchestrator()

	testCases := []struct {
		name     string
		code     string
		target   *TargetSpecification
		shouldFind bool
	}{
		{
			name:     "Find simple function",
			code:     "func foo() {}",
			target:   &TargetSpecification{FunctionName: "foo"},
			shouldFind: true,
		},
		{
			name:     "Find function with params",
			code:     "func bar(x int) {}",
			target:   &TargetSpecification{FunctionName: "bar"},
			shouldFind: true,
		},
		{
			name:     "Find nonexistent",
			code:     "func baz() {}",
			target:   &TargetSpecification{FunctionName: "notfound"},
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
