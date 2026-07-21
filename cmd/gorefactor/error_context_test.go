package main

import (
	"encoding/json"
	"testing"
)

func TestDetailedErrorJSONMarshaling(t *testing.T) {
	err := NewDetailedError(ErrVariableOutOfScope, "test error")
	err.WithContext("test.go", 10, 20, "test context")
	err.WithRootCause("reason 1")
	err.WithSuggestion("approach1", "description 1", 0.8)
	err.WithSuggestionCommand("approach2", "description 2", "command", 0.9)
	err.WithDetail("key1", "value1")
	err.WithRelatedCode("snippet", "code here")

	// Marshal to JSON
	data, marshalErr := json.Marshal(err)
	if marshalErr != nil {
		t.Fatalf("Failed to marshal: %v", marshalErr)
	}

	// Verify it's valid JSON
	var m map[string]interface{}
	if unmarshalErr := json.Unmarshal(data, &m); unmarshalErr != nil {
		t.Fatalf("Failed to unmarshal JSON: %v", unmarshalErr)
	}

	// Verify key fields
	if m["code"] != "VARIABLE_OUT_OF_SCOPE" {
		t.Errorf("Expected code VARIABLE_OUT_OF_SCOPE, got %v", m["code"])
	}

	if m["message"] != "test error" {
		t.Errorf("Expected message 'test error', got %v", m["message"])
	}

	// Verify context
	ctx := m["context"].(map[string]interface{})
	if ctx["file"] != "test.go" {
		t.Errorf("Expected file 'test.go', got %v", ctx["file"])
	}

	// Verify suggestions are sorted by likelihood
	suggestions := m["suggestions"].([]interface{})
	if len(suggestions) != 2 {
		t.Errorf("Expected 2 suggestions, got %d", len(suggestions))
	}

	// First suggestion should have higher likelihood (0.9 > 0.8)
	firstSugg := suggestions[0].(map[string]interface{})
	if firstSugg["likelihood"] != 0.9 {
		t.Errorf("Expected first suggestion likelihood 0.9, got %v", firstSugg["likelihood"])
	}

	t.Logf("JSON output:\n%s", string(data))
}

func TestErrorSorting(t *testing.T) {
	err := NewDetailedError(ErrVariableOutOfScope, "test")

	// Add in non-sorted order
	err.WithSuggestion("low", "low likelihood", 0.3)
	err.WithSuggestion("high", "high likelihood", 0.9)
	err.WithSuggestion("mid", "mid likelihood", 0.6)

	// Verify sorted correctly
	if len(err.Suggestions) != 3 {
		t.Errorf("Expected 3 suggestions, got %d", len(err.Suggestions))
	}

	if err.Suggestions[0].Likelihood != 0.9 {
		t.Errorf("Expected first suggestion likelihood 0.9, got %v", err.Suggestions[0].Likelihood)
	}

	if err.Suggestions[1].Likelihood != 0.6 {
		t.Errorf("Expected second suggestion likelihood 0.6, got %v", err.Suggestions[1].Likelihood)
	}

	if err.Suggestions[2].Likelihood != 0.3 {
		t.Errorf("Expected third suggestion likelihood 0.3, got %v", err.Suggestions[2].Likelihood)
	}
}

func TestVariableOutOfScopeErrorExample(t *testing.T) {
	defs := map[string]int{
		"config": 40,
		"logger": 35,
	}

	err := ExampleVariableOutOfScopeError("orders.go", 50, 75, []string{"config", "logger"}, defs)

	if err.Code != ErrVariableOutOfScope {
		t.Errorf("Expected code %s, got %s", ErrVariableOutOfScope, err.Code)
	}

	if len(err.RootCauses) != 2 {
		t.Errorf("Expected 2 root causes, got %d", len(err.RootCauses))
	}

	if len(err.Suggestions) < 3 {
		t.Errorf("Expected at least 3 suggestions, got %d", len(err.Suggestions))
	}

	// Verify suggestions are sorted by likelihood
	if err.Suggestions[0].Likelihood < err.Suggestions[1].Likelihood {
		t.Error("Suggestions not sorted by likelihood (descending)")
	}

	// Test JSON marshaling
	data, marshalErr := json.Marshal(err)
	if marshalErr != nil {
		t.Fatalf("Failed to marshal: %v", marshalErr)
	}

	// Verify JSON is valid and parseable
	var result map[string]interface{}
	if unmarshalErr := json.Unmarshal(data, &result); unmarshalErr != nil {
		t.Fatalf("Failed to unmarshal: %v", unmarshalErr)
	}

	t.Logf("Example error JSON:\n%s", string(data))
}

func TestReturnStatementErrorExample(t *testing.T) {
	err := ExampleReturnStatementError("handlers.go", 50, 75, []int{62, 70})

	if err.Code != ErrReturnStatementInBlock {
		t.Errorf("Expected code %s, got %s", ErrReturnStatementInBlock, err.Code)
	}

	if len(err.Suggestions) == 0 {
		t.Error("Expected at least one suggestion")
	}

	// Test JSON marshaling
	data, marshalErr := json.Marshal(err)
	if marshalErr != nil {
		t.Fatalf("Failed to marshal: %v", marshalErr)
	}

	var result map[string]interface{}
	if unmarshalErr := json.Unmarshal(data, &result); unmarshalErr != nil {
		t.Fatalf("Failed to unmarshal: %v", unmarshalErr)
	}

	t.Logf("Return statement error JSON:\n%s", string(data))
}

func TestDetailedErrorErrorInterface(t *testing.T) {
	err := NewDetailedError(ErrVariableOutOfScope, "test message")
	var e error = err

	if e.Error() != "test message" {
		t.Errorf("Expected error message 'test message', got %v", e.Error())
	}

	if !isDetailedError(e) {
		t.Error("isDetailedError should return true")
	}

	if asDetailedError(e) == nil {
		t.Error("asDetailedError should return non-nil")
	}

	// Test with regular error
	regularErr := NewDetailedError(ErrGeneric, "generic")
	if !isDetailedError(regularErr) {
		t.Error("isDetailedError should work with DetailedError instances")
	}
}

func TestDetailedErrorChaining(t *testing.T) {
	err := NewDetailedError(ErrVariableOutOfScope, "test").
		WithContext("file.go", 1, 10, "context").
		WithRootCause("cause1").
		WithRootCause("cause2").
		WithSuggestion("app1", "desc1", 0.9).
		WithSuggestionCommand("app2", "desc2", "cmd", 0.8).
		WithDetail("key", "value").
		WithRelatedCode("code", "snippet")

	if err.Message != "test" {
		t.Error("Chaining broke message")
	}

	if len(err.RootCauses) != 2 {
		t.Error("Chaining broke root causes")
	}

	if len(err.Suggestions) != 2 {
		t.Error("Chaining broke suggestions")
	}

	if err.Details["key"] != "value" {
		t.Error("Chaining broke details")
	}

	if err.RelatedCode["code"] != "snippet" {
		t.Error("Chaining broke related code")
	}
}
