package analyzer

import (
	"encoding/json"
	"testing"
)

func TestGenerateRefactoringPlan_ValidFallbackStrategies(t *testing.T) {
	da := NewDiffAnalyzer()

	// Create a test change
	changes := []*Change{
		{
			Type:        "function_addition",
			File:        "test.go",
			Description: "Added test function",
			StartLine:   10,
			EndLine:     15,
			Confidence:  0.9,
			Details: map[string]interface{}{
				"functionName": "testFunction",
				"code":         "func testFunction() {}",
			},
		},
	}

	plan := da.generateRefactoringPlan(changes)

	if plan == nil {
		t.Fatal("Generated plan is nil")
	}

	if len(plan.Operations) != 1 {
		t.Fatalf("Expected 1 operation, got %d", len(plan.Operations))
	}

	operation := plan.Operations[0]

	// Check that fallback strategy is valid
	if operation.Fallback == nil {
		t.Fatal("Fallback strategy is nil")
	}

	validFallbacks := map[string]bool{
		"skip":        true,
		"use_default": true,
	}

	if !validFallbacks[operation.Fallback.Type] {
		t.Errorf("Invalid fallback strategy: %s. Valid options are: skip, use_default", operation.Fallback.Type)
	}
}

func TestGenerateRefactoringPlan_Metadata(t *testing.T) {
	da := NewDiffAnalyzer()

	changes := []*Change{
		{
			Type:        "function_addition",
			File:        "test.go",
			Description: "Added test function",
			StartLine:   10,
			EndLine:     15,
			Confidence:  0.9,
			Details: map[string]interface{}{
				"functionName": "testFunction",
				"code":         "func testFunction() {}",
			},
		},
	}

	plan := da.generateRefactoringPlan(changes)

	// Check metadata
	if plan.Metadata == nil {
		t.Fatal("Plan metadata is nil")
	}

	source, exists := plan.Metadata["source"]
	if !exists {
		t.Fatal("Source metadata not found")
	}

	if source != "diff_analysis" {
		t.Errorf("Expected source 'diff_analysis', got '%s'", source)
	}

	changesCount, exists := plan.Metadata["changes"]
	if !exists {
		t.Fatal("Changes count metadata not found")
	}

	if changesCount != 1 {
		t.Errorf("Expected changes count 1, got %v", changesCount)
	}
}

func TestGenerateSummary(t *testing.T) {
	da := NewDiffAnalyzer()

	testCases := []struct {
		changes  []*Change
		expected string
	}{
		{
			[]*Change{},
			"No changes detected",
		},
		{
			[]*Change{
				{Type: "function_addition"},
			},
			"Detected 1 changes:\n- 1 function_addition\n",
		},
		{
			[]*Change{
				{Type: "function_addition"},
				{Type: "method_addition"},
				{Type: "function_addition"},
			},
			"Detected 3 changes:\n- 2 function_addition\n- 1 method_addition\n",
		},
	}

	for i, tc := range testCases {
		result := da.generateSummary(tc.changes)
		if result != tc.expected {
			t.Errorf("Test case %d: expected '%s', got '%s'", i+1, tc.expected, result)
		}
	}
}

func TestChangeToOperation_ValidOperationTypes(t *testing.T) {
	da := NewDiffAnalyzer()

	testCases := []struct {
		changeType   string
		shouldReturn bool
	}{
		{"function_addition", true},
		{"method_addition", true},
		{"interface_addition", true},
		{"struct_addition", true},
		{"code_insertion", true},
		{"variable_rename", true},
		{"function_modification", true},
		{"unknown_type", false},
	}

	for _, tc := range testCases {
		change := &Change{
			Type:        tc.changeType,
			File:        "test.go",
			Description: "Test change",
			StartLine:   10,
			EndLine:     15,
			Confidence:  0.9,
			Details: map[string]interface{}{
				"code": "test code",
			},
		}

		operation := da.changeToOperation(change)

		if tc.shouldReturn && operation == nil {
			t.Errorf("Expected operation for type '%s', got nil", tc.changeType)
		}

		if !tc.shouldReturn && operation != nil {
			t.Errorf("Expected nil for type '%s', got operation", tc.changeType)
		}
	}
}

func TestPlanSerialization(t *testing.T) {
	da := NewDiffAnalyzer()

	changes := []*Change{
		{
			Type:        "function_addition",
			File:        "test.go",
			Description: "Added test function",
			StartLine:   10,
			EndLine:     15,
			Confidence:  0.9,
			Details: map[string]interface{}{
				"functionName": "testFunction",
				"code":         "func testFunction() {}",
			},
		},
	}

	plan := da.generateRefactoringPlan(changes)

	// Test that the plan can be serialized to JSON
	_, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("Failed to serialize plan to JSON: %v", err)
	}
}

func TestPlanValidation(t *testing.T) {
	da := NewDiffAnalyzer()

	changes := []*Change{
		{
			Type:        "function_addition",
			File:        "test.go",
			Description: "Added test function",
			StartLine:   10,
			EndLine:     15,
			Confidence:  0.9,
			Details: map[string]interface{}{
				"functionName": "testFunction",
				"code":         "func testFunction() {}",
			},
		},
	}

	plan := da.generateRefactoringPlan(changes)

	// Validate plan structure
	if plan.Version == "" {
		t.Error("Plan version is empty")
	}

	if plan.Name == "" {
		t.Error("Plan name is empty")
	}

	if plan.Description == "" {
		t.Error("Plan description is empty")
	}

	if plan.Created.IsZero() {
		t.Error("Plan created time is zero")
	}

	if plan.Author == "" {
		t.Error("Plan author is empty")
	}

	if plan.Operations == nil {
		t.Error("Plan operations is nil")
	}

	// Validate each operation
	for i, operation := range plan.Operations {
		if operation.Type == "" {
			t.Errorf("Operation %d: Type is empty", i+1)
		}

		if operation.Description == "" {
			t.Errorf("Operation %d: Description is empty", i+1)
		}

		if operation.File == "" {
			t.Errorf("Operation %d: File is empty", i+1)
		}

		if operation.Target == nil {
			t.Errorf("Operation %d: Target is nil", i+1)
		}

		if operation.Parameters == nil {
			t.Errorf("Operation %d: Parameters is nil", i+1)
		}
	}
}
