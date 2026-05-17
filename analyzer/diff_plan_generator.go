package analyzer

import (
	"fmt"
	"github.com/chrisophus/gorefactor/orchestrator"
	"sort"
	"strings"
	"time"
)

// consolidateChanges consolidates related changes (e.g., multiple renames of the same variable)
func (da *DiffAnalyzer) consolidateChanges(changes []*Change) []*Change {
	if len(changes) <= 1 {
		return changes
	}

	// Check if all changes are variable_rename with the same old/new names
	if allVariableRenames(changes) {
		// Get the first change's old/new names
		oldName := changes[0].Details["oldName"]
		newName := changes[0].Details["newName"]

		// Check if all renames are the same
		allSameRename := true
		for _, change := range changes {
			if change.Details["oldName"] != oldName || change.Details["newName"] != newName {
				allSameRename = false
				break
			}
		}

		if allSameRename && len(changes) > 1 {
			// Consolidate into single change with first and last line numbers
			consolidated := &Change{
				Type:        "variable_rename",
				File:        changes[0].File,
				Description: fmt.Sprintf("Renamed variable %v to %v", oldName, newName),
				StartLine:   changes[0].StartLine,
				EndLine:     changes[len(changes)-1].EndLine,
				Confidence:  0.8,
				Details: map[string]interface{}{
					"oldName":     oldName,
					"newName":     newName,
					"occurrences": len(changes),
				},
			}
			return []*Change{consolidated}
		}
	}

	return changes
}

// allVariableRenames checks if all changes are variable_rename type
func allVariableRenames(changes []*Change) bool {
	if len(changes) == 0 {
		return false
	}
	for _, change := range changes {
		if change.Type != "variable_rename" {
			return false
		}
	}
	return true
}

// generateSummary generates a summary of the changes
func (da *DiffAnalyzer) generateSummary(changes []*Change) string {
	if len(changes) == 0 {
		return "No changes detected"
	}

	var summary strings.Builder
	fmt.Fprintf(&summary, "Detected %d changes:\n", len(changes))

	changeTypes := make(map[string]int)
	for _, change := range changes {
		changeTypes[change.Type]++
	}

	// Sort change types alphabetically for consistent output
	var types []string
	for changeType := range changeTypes {
		types = append(types, changeType)
	}
	sort.Strings(types)

	for _, changeType := range types {
		fmt.Fprintf(&summary, "- %d %s\n", changeTypes[changeType], changeType)
	}

	return summary.String()
}

// generateRefactoringPlan generates a refactoring plan from the changes
func (da *DiffAnalyzer) generateRefactoringPlan(changes []*Change) *orchestrator.RefactoringPlan {
	plan := &orchestrator.RefactoringPlan{
		Version:     "1.0",
		Name:        "generated_from_diff",
		Description: "Refactoring plan generated from code diff analysis",
		Created:     time.Now(),
		Author:      "DiffAnalyzer",
		Operations:  []*orchestrator.RefactoringOperation{},
		Metadata: map[string]interface{}{
			"source":  "diff_analysis",
			"changes": len(changes),
		},
	}

	for _, change := range changes {
		operation := da.changeToOperation(change)
		if operation != nil {
			plan.Operations = append(plan.Operations, operation)
		}
	}

	return plan
}

// changeToOperation converts a change to a refactoring operation
func (da *DiffAnalyzer) changeToOperation(change *Change) *orchestrator.RefactoringOperation {
	switch change.Type {
	case "function_addition":
		return da.createInsertCodeOperation(change)
	case "method_addition":
		return da.createInsertCodeOperation(change)
	case "interface_addition":
		return da.createInsertCodeOperation(change)
	case "struct_addition":
		return da.createInsertCodeOperation(change)
	case "code_insertion":
		return da.createInsertCodeOperation(change)
	case "variable_rename":
		return da.createRenameVariableOperation(change)
	case "function_modification":
		return da.createExtractMethodOperation(change)
	default:
		return nil
	}
}
