package orchestrator

import (
	"os"
	"strings"
	"testing"
)

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
