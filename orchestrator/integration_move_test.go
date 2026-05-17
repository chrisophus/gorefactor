package orchestrator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIntegration_MoveMethod(t *testing.T) {
	tmpDir := os.TempDir()
	testDir := filepath.Join(tmpDir, "integration_test_move")
	if err := os.MkdirAll(testDir, 0755); err != nil {
		t.Fatalf("Failed to create test dir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(testDir)
	})

	// Create source file
	sourceFile := filepath.Join(testDir, "source.go")
	sourceCode := `package handlers

type Handler struct{}

func (h *Handler) Process() error {
	return nil
}
`
	if err := os.WriteFile(sourceFile, []byte(sourceCode), 0644); err != nil {
		t.Fatalf("Failed to write source file: %v", err)
	}

	// Create destination file
	destFile := filepath.Join(testDir, "dest.go")
	destCode := `package handlers
`
	if err := os.WriteFile(destFile, []byte(destCode), 0644); err != nil {
		t.Fatalf("Failed to write dest file: %v", err)
	}

	// Create plan: move method
	planFile := filepath.Join(testDir, "plan.json")
	plan := &RefactoringPlan{
		Version:     "1.0",
		Name:        "move_workflow",
		Description: "Move handler method",
		Operations: []*RefactoringOperation{
			{
				Type:        "move_method",
				Description: "Move Process method to dest file",
				File:        sourceFile,
				Target: &TargetSpecification{
					MethodName:   "Process",
					ReceiverType: "Handler",
				},
				Parameters: map[string]interface{}{
					"newFile": destFile,
				},
			},
		},
	}

	planJSON, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal plan: %v", err)
	}
	if err := os.WriteFile(planFile, planJSON, 0644); err != nil {
		t.Fatalf("Failed to write plan file: %v", err)
	}

	// Load and execute plan
	orch := NewOrchestrator()
	_, err = orch.LoadPlan(planFile)
	if err != nil {
		t.Fatalf("Failed to load plan: %v", err)
	}

	result, err := orch.ExecutePlan("move_workflow")
	if err != nil {
		t.Fatalf("ExecutePlan failed: %v", err)
	}

	// Verify execution
	if !result.Success {
		t.Errorf("Plan execution failed. Errors: %v", result.Errors)
	}

	// Verify source file was modified
	sourceContent, _ := os.ReadFile(sourceFile)
	if strings.Contains(string(sourceContent), "func (h *Handler) Process()") {
		t.Error("Method should be removed from source file")
	}

	// Verify destination file was modified
	destContent, _ := os.ReadFile(destFile)
	if !strings.Contains(string(destContent), "func (h *Handler) Process()") {
		t.Error("Method should be added to destination file")
	}
}
