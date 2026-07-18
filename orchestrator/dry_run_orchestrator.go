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
	before := make(map[string]string, len(affected))
	for _, f := range affected {
		b, err := os.ReadFile(f)
		if err == nil {
			before[f] = string(b)
		}
	}
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

	// Map real path → temp path.
	pathMap := make(map[string]string, len(before))
	if r0, r1, done := extractBlockL89(before, tempDir, pathMap); done {
		return r0, r1
	}

	// Clone the operation, rewriting all file references to temp paths.
	cloned := cloneOperationWithPaths(operation, pathMap)

	// Execute the cloned operation using a fresh, snapshot-less orchestrator.
	inner := NewOrchestrator()
	inner.SkipSnapshot = true
	execResult, execErr := inner.ExecuteOperations([]*RefactoringOperation{cloned})
	if execErr != nil {
		return nil, fmt.Errorf("dry-run execution failed: %w", execErr)
	}
	if execResult != nil && !execResult.Success && len(execResult.Errors) > 0 {
		return nil, fmt.Errorf("dry-run execution failed: %s", strings.Join(execResult.Errors, "; "))
	}

	// Compare temp files to the original snapshots to produce per-file diffs.
	var diffs []*FileDiff
	diffs = extractBlockL109(pathMap, before, diffs, operation)

	// Also capture files created by the operation (e.g. move destination).
	if newFile, ok := operation.Parameters["newFile"].(string); ok && newFile != "" {
		if tmpNewFile, ok := pathMap[newFile]; !ok {
			// Destination was not in our snapshot (new file); look in temp dir.
			tmpCreated := filepath.Join(tempDir, filepath.Base(newFile))
			if content, err := os.ReadFile(tmpCreated); err == nil {
				diffs = append(diffs, &FileDiff{
					File:    newFile,
					OldCode: "",
					NewCode: string(content),
					Summary: fmt.Sprintf("Created by %s", operation.Type),
				})
			}
		} else {
			// Already captured above via pathMap; ensure we didn't miss it.
			_ = tmpNewFile
		}
	}

	return diffs, nil
}

func extractBlockL109(pathMap map[string]string, before map[string]string, diffs []*FileDiff, operation *RefactoringOperation) []*FileDiff {
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
	return diffs
}

func extractBlockL89(before map[string]string, tempDir string, pathMap map[string]string) (r0 []*FileDiff, r1 error, done bool) {
	for realPath, content := range before {
		// Preserve the base name so package-clause detection works.
		tmpFile := filepath.Join(tempDir, filepath.Base(realPath))
		// Disambiguate when two files share a basename.
		for i := 1; ; i++ {
			if _, exists := pathMap[tmpFile]; !exists {
				break
			}
			tmpFile = filepath.Join(tempDir, fmt.Sprintf("%d_%s", i, filepath.Base(realPath)))
		}
		if err := os.WriteFile(tmpFile, []byte(content), 0600); err != nil {
			return nil, fmt.Errorf("dry-run: write temp file: %w", err), true
		}
		pathMap[realPath] = tmpFile
	}
	return
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
