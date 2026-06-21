package main

import (
	"encoding/json"
	"testing"
)

// TestExtractErrorDetailsInJSON verifies ErrorDetails appear in JSON output
func TestExtractErrorDetailsInJSON(t *testing.T) {
	// Create a simple test that returns error
	testErr := NewDetailedError(ErrReturnStatementInBlock,
		"Cannot extract: block contains return statement").
		WithContext("test.go", 1, 5, "test").
		WithSuggestion("test", "test suggestion", 0.9)

	// Verify it serializes to JSON properly
	result := mutationResult{
		Success:      false,
		Operation:    "extract",
		File:         "test.go",
		Error:        testErr.Error(),
		ErrorDetails: testErr,
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	// Verify it can be unmarshaled
	var unmarshaled map[string]interface{}
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	// Verify errorDetails is present
	if _, ok := unmarshaled["errorDetails"]; !ok {
		t.Error("errorDetails not found in JSON output")
	}

	// Verify it matches expected structure
	if success, ok := unmarshaled["success"].(bool); !ok || success {
		t.Error("success should be false")
	}

	if operation, ok := unmarshaled["operation"].(string); !ok || operation != "extract" {
		t.Error("operation should be extract")
	}

	t.Logf("JSON output:\n%s", string(data))
}

// TestReturnStatementErrorBuilder verifies ExampleReturnStatementError works correctly
func TestReturnStatementErrorBuilder(t *testing.T) {
	err := ExampleReturnStatementError("test.go", 10, 20, []int{15, 18})

	if string(err.Code) != "RETURN_IN_BLOCK" {
		t.Errorf("Expected code RETURN_IN_BLOCK, got %s", err.Code)
	}

	if err.Context == nil {
		t.Fatal("Expected Context to be set")
	}

	if err.Context.File != "test.go" {
		t.Errorf("Expected file test.go, got %s", err.Context.File)
	}

	if len(err.Suggestions) < 2 {
		t.Errorf("Expected at least 2 suggestions, got %d", len(err.Suggestions))
	}

	// Verify suggestions are sorted
	for i := 0; i < len(err.Suggestions)-1; i++ {
		if err.Suggestions[i].Likelihood < err.Suggestions[i+1].Likelihood {
			t.Error("Suggestions not sorted by likelihood (descending)")
		}
	}

	// Verify root causes are present
	if len(err.RootCauses) == 0 {
		t.Error("Expected root causes")
	}

	t.Logf("Error: %+v", err)
}

// TestVariableOutOfScopeErrorBuilder verifies example builder works
func TestVariableOutOfScopeErrorBuilder(t *testing.T) {
	defs := map[string]int{
		"config": 40,
		"logger": 35,
	}

	err := ExampleVariableOutOfScopeError("orders.go", 50, 75, []string{"config", "logger"}, defs)

	if string(err.Code) != "VARIABLE_OUT_OF_SCOPE" {
		t.Errorf("Expected VARIABLE_OUT_OF_SCOPE, got %s", err.Code)
	}

	if len(err.Suggestions) < 2 {
		t.Errorf("Expected at least 2 suggestions, got %d", len(err.Suggestions))
	}

	if len(err.RootCauses) != 2 {
		t.Errorf("Expected 2 root causes, got %d", len(err.RootCauses))
	}

	// First suggestion should be add_parameter with high likelihood
	if err.Suggestions[0].Approach != "add_parameter" {
		t.Errorf("Expected first suggestion add_parameter, got %s", err.Suggestions[0].Approach)
	}

	if err.Suggestions[0].Likelihood < 0.9 {
		t.Errorf("Expected high likelihood for add_parameter, got %f", err.Suggestions[0].Likelihood)
	}

	t.Logf("Error: %+v", err)
}
