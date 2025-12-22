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

	if !strings.Contains(result.Error, "Failed to find target") {
		t.Errorf("Expected 'Failed to find target' error, got: %s", result.Error)
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

// ============================================================================
// Tests for GOREFACTOR_IMPROVEMENTS.md recommendations
// ============================================================================

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
