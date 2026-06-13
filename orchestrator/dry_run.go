package orchestrator

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
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
		opResult.Success = false
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
	defer os.RemoveAll(tempDir)

	// Map real path → temp path.
	pathMap := make(map[string]string, len(before))
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
			return nil, fmt.Errorf("dry-run: write temp file: %w", err)
		}
		pathMap[realPath] = tmpFile
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
	for realPath, tmpPath := range pathMap {
		newContent, err := os.ReadFile(tmpPath)
		if err != nil {
			// Temp file may have been deleted (e.g. source removed during move).
			newContent = nil
		}
		oldContent := before[realPath]
		newStr := string(newContent)
		if oldContent == newStr {
			continue // no change for this file
		}
		diffs = append(diffs, &FileDiff{
			File:    realPath,
			OldCode: oldContent,
			NewCode: newStr,
			Summary: fmt.Sprintf("Operation: %s", operation.Type),
		})
	}

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

// dryRunAffectedFiles returns all files an operation may read or write,
// including the destination file for move operations.
func dryRunAffectedFiles(op *RefactoringOperation) []string {
	seen := map[string]bool{}
	var files []string
	add := func(f string) {
		if f != "" && !seen[f] {
			seen[f] = true
			files = append(files, f)
		}
	}
	add(op.File)
	if op.Parameters != nil {
		if nf, ok := op.Parameters["newFile"].(string); ok {
			add(nf)
		}
	}
	return files
}

// cloneOperationWithPaths returns a shallow copy of op with all file paths
// rewritten according to pathMap (real path → temp path).
func cloneOperationWithPaths(op *RefactoringOperation, pathMap map[string]string) *RefactoringOperation {
	remap := func(p string) string {
		if tmp, ok := pathMap[p]; ok {
			return tmp
		}
		return p
	}

	cloned := *op
	cloned.File = remap(op.File)

	if op.Parameters != nil {
		newParams := make(map[string]interface{}, len(op.Parameters))
		for k, v := range op.Parameters {
			if k == "newFile" {
				if s, ok := v.(string); ok {
					newParams[k] = remap(s)
					continue
				}
			}
			newParams[k] = v
		}
		cloned.Parameters = newParams
	}
	return &cloned
}

// copyFileForDryRun copies src to dst; used when we need an exact replica.
func copyFileForDryRun(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
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

// FormatDryRunDiff returns a colorized diff representation
func FormatDryRunDiff(diff *FileDiff) string {
	var output strings.Builder
	fmt.Fprintf(&output, "\n--- %s\n", diff.File)
	fmt.Fprintf(&output, "+++ %s\n", diff.File)

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
				fmt.Fprintf(&output, "- %s\n", oldLine)
			}
			if newLine != "" {
				fmt.Fprintf(&output, "+ %s\n", newLine)
			}
		} else {
			fmt.Fprintf(&output, "  %s\n", oldLine)
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
			fmt.Fprintf(&report, "\n[FAILED] %s: %s\n", op.Operation.Type, op.Error)
			continue
		}
		for _, diff := range op.Changes {
			report.WriteString(FormatDryRunDiff(diff))
		}
	}

	return os.WriteFile(outputPath, []byte(report.String()), 0644)
}
