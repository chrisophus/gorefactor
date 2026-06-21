package main

import (
	"encoding/json"
	"testing"
)

// TestMoveErrorDetails verifies move command returns structured errors
func TestMoveErrorDetails(t *testing.T) {
	// Test that DetailedError is properly serialized in JSON output
	testErr := NewDetailedError(ErrFunctionNotFound,
		"Cannot find target: TestFunc not found in test.go").
		WithContext("test.go", 0, 0, "Function not found").
		WithSuggestion("verify_name", "Check spelling", 0.95)

	result := mutationResult{
		Success:      false,
		Operation:    "move",
		File:         "test.go",
		Error:        testErr.Error(),
		ErrorDetails: testErr,
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	// Verify JSON is valid and contains errorDetails
	var unmarshaled map[string]interface{}
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if _, ok := unmarshaled["errorDetails"]; !ok {
		t.Error("errorDetails not found in JSON output")
	}

	t.Logf("Move error JSON:\n%s", string(data))
}

// TestTargetNotFoundErrorBuilder verifies error creation for missing targets
func TestTargetNotFoundErrorBuilder(t *testing.T) {
	err := ExampleTargetNotFoundError("handlers.go", "ProcessRequest")

	if string(err.Code) != "FUNCTION_NOT_FOUND" {
		t.Errorf("Expected FUNCTION_NOT_FOUND, got %s", err.Code)
	}

	if err.Context == nil {
		t.Fatal("Expected Context to be set")
	}

	if len(err.Suggestions) < 3 {
		t.Errorf("Expected at least 3 suggestions, got %d", len(err.Suggestions))
	}

	// Verify first suggestion is most likely
	if err.Suggestions[0].Likelihood < 0.9 {
		t.Errorf("Expected first suggestion high likelihood, got %f", err.Suggestions[0].Likelihood)  
	}

	// Verify that we have verify_name, check_file, and list_functions suggestions
	approaches := map[string]bool{}
	for _, s := range err.Suggestions {
		approaches[s.Approach] = true
	}

	expected := []string{"verify_name", "check_file", "list_functions"}
	for _, exp := range expected {
		if !approaches[exp] {
			t.Errorf("Expected suggestion approach %s, but not found", exp)
		}
	}

	// Verify list_functions suggestion has command  
	found := false
	for _, s := range err.Suggestions {
		if s.Approach == "list_functions" && s.Command != "" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected list_functions suggestion with command")
	}

	t.Logf("Error: %+v", err)
}

// TestImportCycleErrorBuilder verifies error creation for circular imports
func TestImportCycleErrorBuilder(t *testing.T) {
	cycle := []string{"handlers.go", "models.go", "handlers.go"}
	err := ExampleImportCycleError("handlers.go", "models.go", "ProcessRequest", cycle)

	if string(err.Code) != "IMPORT_CYCLE" {
		t.Errorf("Expected IMPORT_CYCLE, got %s", err.Code)
	}

	if len(err.RootCauses) < 2 {
		t.Errorf("Expected at least 2 root causes, got %d", len(err.RootCauses))
	}

	if len(err.Suggestions) < 2 {
		t.Errorf("Expected at least 2 suggestions, got %d", len(err.Suggestions))
	}

	// Verify suggestions are sorted by likelihood
	for i := 0; i < len(err.Suggestions)-1; i++ {
		if err.Suggestions[i].Likelihood < err.Suggestions[i+1].Likelihood {
			t.Error("Suggestions not sorted by likelihood (descending)")
		}
	}

	// Verify cycle is in details
	if _, ok := err.Details["importCycle"]; !ok {
		t.Error("Expected importCycle in details")
	}

	t.Logf("Error: %+v", err)
}

// TestMoveErrorSuggestions verifies actionable suggestions for move errors
func TestMoveErrorSuggestions(t *testing.T) {
	tests := []struct {
		name         string
		err          *DetailedError
		minSuggestions int
		expectedCode ErrorCode
	}{
		{
			name: "target_not_found",
			err: ExampleTargetNotFoundError("test.go", "Missing"),
			minSuggestions: 3,
			expectedCode: ErrFunctionNotFound,
		},
		{
			name: "import_cycle",
			err: ExampleImportCycleError("a.go", "b.go", "Func", []string{"a.go", "b.go"}),
			minSuggestions: 3,
			expectedCode: ErrImportCycle,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if string(test.err.Code) != string(test.expectedCode) {
				t.Errorf("Expected code %s, got %s", test.expectedCode, test.err.Code)
			}

			if len(test.err.Suggestions) < test.minSuggestions {
				t.Errorf("Expected at least %d suggestions, got %d",
					test.minSuggestions, len(test.err.Suggestions))
			}

			// All suggestions should have non-empty approach and description
			for i, s := range test.err.Suggestions {
				if s.Approach == "" {
					t.Errorf("Suggestion %d: empty approach", i)
				}
				if s.Description == "" {
					t.Errorf("Suggestion %d: empty description", i)
				}
				if s.Likelihood <= 0 || s.Likelihood > 1.0 {
					t.Errorf("Suggestion %d: invalid likelihood %f", i, s.Likelihood)
				}
			}
		})
	}
}
