package orchestrator

import (
	"fmt"
	"os"
	"strings"
)

// DryRunResult represents the preview of changes without applying them
type DryRunResult struct {
	Plan       *RefactoringPlan
	Operations []*DryRunOperationResult
	Summary    string
}

// DryRunOperationResult shows what would change for a single operation
type DryRunOperationResult struct {
	Operation *RefactoringOperation
	Changes   []*FileDiff
	Success   bool
	Error     string
}

// FileDiff represents the differences for a single file
type FileDiff struct {
	File      string
	OldCode   string
	NewCode   string
	StartLine int
	EndLine   int
	Summary   string
}

// ExecutePlanDryRun executes a plan in dry-run mode without writing files
func (o *Orchestrator) ExecutePlanDryRun(planName string) (*DryRunResult, error) {
	plan, exists := o.plans[planName]
	if !exists {
		return nil, fmt.Errorf("plan '%s' not found", planName)
	}

	result := &DryRunResult{
		Plan:       plan,
		Operations: make([]*DryRunOperationResult, 0, len(plan.Operations)),
	}

	// For each operation, simulate what would change
	for _, operation := range plan.Operations {
		dryOp := o.dryRunOperation(operation)
		result.Operations = append(result.Operations, dryOp)
	}

	result.Summary = o.generateDryRunSummary(result)
	return result, nil
}

// dryRunOperation simulates what a single operation would do
func (o *Orchestrator) dryRunOperation(operation *RefactoringOperation) *DryRunOperationResult {
	opResult := &DryRunOperationResult{
		Operation: operation,
		Changes:   make([]*FileDiff, 0),
	}

	// Read the file contents for simulation
	content, err := os.ReadFile(operation.File)
	if err != nil {
		opResult.Success = false
		opResult.Error = fmt.Sprintf("failed to read file: %v", err)
		return opResult
	}

	oldCode := string(content)

	// Simulate the operation (this would need to be customized per operation type)
	// For now, we'll just record that the file would be changed
	newCode, changed := o.simulateOperationChange(operation, oldCode)

	if changed {
		opResult.Changes = append(opResult.Changes, &FileDiff{
			File:    operation.File,
			OldCode: oldCode,
			NewCode: newCode,
			Summary: fmt.Sprintf("Operation: %s", operation.Type),
		})
		opResult.Success = true
	} else {
		opResult.Success = true
		opResult.Error = "No changes would be made"
	}

	return opResult
}

// simulateOperationChange simulates the change an operation would make
func (o *Orchestrator) simulateOperationChange(operation *RefactoringOperation, oldCode string) (string, bool) {
	// This is a placeholder; actual implementation would depend on operation type
	// For now, we'll just note that changes would occur
	return oldCode, true
}

// generateDryRunSummary creates a human-readable summary of what would change
func (o *Orchestrator) generateDryRunSummary(result *DryRunResult) string {
	var summary strings.Builder
	summary.WriteString(fmt.Sprintf("Dry-run of plan: %s\n", result.Plan.Name))
	summary.WriteString(fmt.Sprintf("Operations: %d\n", len(result.Operations)))

	successCount := 0
	failureCount := 0
	filesAffected := make(map[string]bool)

	for _, op := range result.Operations {
		if op.Success {
			successCount++
		} else {
			failureCount++
		}
		for _, diff := range op.Changes {
			filesAffected[diff.File] = true
		}
	}

	summary.WriteString(fmt.Sprintf("Would succeed: %d\n", successCount))
	summary.WriteString(fmt.Sprintf("Would fail: %d\n", failureCount))
	summary.WriteString(fmt.Sprintf("Files affected: %d\n", len(filesAffected)))

	return summary.String()
}

// FormatDryRunDiff returns a colorized diff representation
func FormatDryRunDiff(diff *FileDiff) string {
	var output strings.Builder
	output.WriteString(fmt.Sprintf("\n--- %s\n", diff.File))
	output.WriteString(fmt.Sprintf("+++ %s\n", diff.File))

	oldLines := strings.Split(diff.OldCode, "\n")
	newLines := strings.Split(diff.NewCode, "\n")

	// Simple line-by-line diff (naive implementation)
	maxLines := len(oldLines)
	if len(newLines) > maxLines {
		maxLines = len(newLines)
	}

	for i := 0; i < maxLines; i++ {
		oldLine := ""
		newLine := ""

		if i < len(oldLines) {
			oldLine = oldLines[i]
		}
		if i < len(newLines) {
			newLine = newLines[i]
		}

		if oldLine != newLine {
			if oldLine != "" {
				output.WriteString(fmt.Sprintf("- %s\n", oldLine))
			}
			if newLine != "" {
				output.WriteString(fmt.Sprintf("+ %s\n", newLine))
			}
		} else {
			output.WriteString(fmt.Sprintf("  %s\n", oldLine))
		}
	}

	return output.String()
}

// DiffPaths returns affected file paths from a dry-run result
func (d *DryRunResult) DiffPaths() []string {
	seen := make(map[string]bool)
	var paths []string
	for _, op := range d.Operations {
		for _, diff := range op.Changes {
			if !seen[diff.File] {
				seen[diff.File] = true
				paths = append(paths, diff.File)
			}
		}
	}
	return paths
}

// SaveDryRunReport saves a dry-run report to a file
func SaveDryRunReport(result *DryRunResult, outputPath string) error {
	var report strings.Builder
	report.WriteString(result.Summary)
	report.WriteString("\n\n=== Changes Preview ===\n")

	for _, op := range result.Operations {
		if !op.Success {
			report.WriteString(fmt.Sprintf("\n[FAILED] %s: %s\n", op.Operation.Type, op.Error))
			continue
		}
		for _, diff := range op.Changes {
			report.WriteString(FormatDryRunDiff(diff))
		}
	}

	return os.WriteFile(outputPath, []byte(report.String()), 0644)
}
