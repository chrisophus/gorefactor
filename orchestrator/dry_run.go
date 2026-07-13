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
