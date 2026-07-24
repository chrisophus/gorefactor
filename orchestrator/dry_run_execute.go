package orchestrator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

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

// dryRunOperation simulates what a single operation would do by running the
// transformation against in-memory copies of all affected files and computing
// per-file diffs. The original files are never modified.
func (o *Orchestrator) dryRunOperation(operation *RefactoringOperation) *DryRunOperationResult {
	opResult := &DryRunOperationResult{
		Operation: operation,
		Changes:   make([]*FileDiff, 0),
	}

	diffs, err := o.simulateOperationChange(operation)
	if err != nil {

		opResult.Error = err.Error()
		return opResult
	}

	if len(diffs) == 0 {
		opResult.Success = true
		opResult.Error = "No changes would be made"
		return opResult
	}

	opResult.Changes = diffs
	opResult.Success = true
	return opResult
}

// simulateOperationChange runs an operation's transformation against temporary
// copies of the affected files and returns per-file diffs without writing back
// to the originals. It returns an error if the operation itself would fail.
func (o *Orchestrator) simulateOperationChange(operation *RefactoringOperation) ([]*FileDiff, error) {
	// Collect the set of files this operation may touch so we can snapshot them.
	affected := dryRunAffectedFiles(operation)

	// Snapshot the pre-operation content for every file that exists.
	before := snapshotDryRunFiles(affected)
	if len(before) == 0 && operation.File != "" {
		// Source file does not exist — cannot simulate.
		return nil, fmt.Errorf("source file %s not found", operation.File)
	}

	// Build a sandboxed copy: rewrite the operation's file references to temp
	// paths so the real orchestrator runs in a throw-away directory.
	tempDir, err := os.MkdirTemp("", "gorefactor-dryrun-*")
	if err != nil {
		return nil, fmt.Errorf("dry-run: create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	pathMap, err := writeDryRunSandboxFiles(tempDir, before)
	if err != nil {
		return nil, err
	}
	ensureDryRunGoMod(tempDir)

	// Clone the operation, rewriting all file references to temp paths.
	cloned := cloneOperationWithPaths(operation, pathMap)

	// Execute the cloned operation in the sandbox with a throw-away orchestrator.
	inner := NewOrchestrator()
	execResult, execErr := inner.ExecuteOperations([]*RefactoringOperation{cloned})
	if execErr != nil {
		return nil, fmt.Errorf("dry-run execution failed: %w", execErr)
	}
	if execResult != nil && !execResult.Success && len(execResult.Errors) > 0 {
		return nil, fmt.Errorf("dry-run execution failed: %s", strings.Join(execResult.Errors, "; "))
	}

	return collectDryRunDiffs(operation, before, pathMap, tempDir), nil
}
func writeDryRunSandboxFiles(tempDir string, before map[string]string) (map[string]string, error) {
	pathMap := make(map[string]string, len(before))
	for realPath, content := range before {

		tmpFile := filepath.Join(tempDir, filepath.Base(realPath))

		for i := 1; ; i++ {
			if _, exists := pathMap[tmpFile]; !exists {
				break
			}
			tmpFile = filepath.Join(tempDir, fmt.Sprintf("%d_%s", i, filepath.Base(realPath)))
		}
		if err := os.WriteFile(tmpFile, []byte(content), 0600); err != nil {
			return nil, fmt.Errorf("dry-run: write temp file: %w", err)
		}
		pathMap[realPath] = tmpFile
	}
	return pathMap, nil
}

func ensureDryRunGoMod(tempDir string) {

	if _, statErr := os.Stat(filepath.Join(tempDir, "go.mod")); os.IsNotExist(statErr) {
		_ = os.WriteFile(filepath.Join(tempDir, "go.mod"), []byte("module gorefactordryrun\n\ngo 1.21\n"), 0600)
	}
}

func collectDryRunDiffs(operation *RefactoringOperation, before map[string]string, pathMap map[string]string, tempDir string) []*FileDiff {
	var diffs []*FileDiff
	for realPath, tmpPath := range pathMap {
		newContent, err := os.ReadFile(tmpPath)
		if err != nil {
			newContent = nil
		}
		oldContent := before[realPath]
		newStr := string(newContent)
		if oldContent == newStr {
			continue
		}
		diffs = append(diffs, &FileDiff{
			File:    realPath,
			OldCode: oldContent,
			NewCode: newStr,
			Summary: fmt.Sprintf("Operation: %s", operation.Type),
		})
	}

	if newFile, ok := operation.Parameters["newFile"].(string); ok && newFile != "" {
		if _, snapshotted := pathMap[newFile]; !snapshotted {

			tmpCreated := filepath.Join(tempDir, filepath.Base(newFile))
			if content, err := os.ReadFile(tmpCreated); err == nil {
				diffs = append(diffs, &FileDiff{
					File:    newFile,
					OldCode: "",
					NewCode: string(content),
					Summary: fmt.Sprintf("Created by %s", operation.Type),
				})
			}
		}
	}
	return diffs
}

func snapshotDryRunFiles(affected []string) map[string]string {
	before := make(map[string]string, len(affected))
	for _, f := range affected {
		b, err := os.ReadFile(f)
		if err == nil {
			before[f] = string(b)
		}
	}
	return before
}

// generateDryRunSummary creates a human-readable summary of what would change
func (o *Orchestrator) generateDryRunSummary(result *DryRunResult) string {
	var summary strings.Builder
	fmt.Fprintf(&summary, "Dry-run of plan: %s\n", result.Plan.Name)
	fmt.Fprintf(&summary, "Operations: %d\n", len(result.Operations))

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

	fmt.Fprintf(&summary, "Would succeed: %d\n", successCount)
	fmt.Fprintf(&summary, "Would fail: %d\n", failureCount)
	fmt.Fprintf(&summary, "Files affected: %d\n", len(filesAffected))

	return summary.String()
}
