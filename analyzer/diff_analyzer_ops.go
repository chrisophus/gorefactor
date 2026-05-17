package analyzer

import (
	"fmt"
	"gorefactor/orchestrator"
	"strings"
)

// analyzeAddedCode analyzes added code to detect patterns
func (da *DiffAnalyzer) analyzeAddedCode(filePath string, hunk *DiffHunk, addedLines []string) *Change {
	code := strings.Join(addedLines, "\n")

	if change := da.detectMethodAdditionChange(filePath, hunk, code); change != nil {
		return change
	}
	if change := da.detectFunctionAdditionChange(filePath, hunk, code); change != nil {
		return change
	}
	if change := da.detectInterfaceAdditionChange(filePath, hunk, code); change != nil {
		return change
	}
	if change := da.detectStructAdditionChange(filePath, hunk, code); change != nil {
		return change
	}

	return &Change{
		Type:        "code_insertion",
		File:        filePath,
		Description: "Inserted new code",
		StartLine:   hunk.StartLine,
		EndLine:     hunk.EndLine,
		Confidence:  0.7,
		Details: map[string]interface{}{
			"code": code,
		},
	}
}

// analyzeRemovedCode analyzes removed code to detect patterns
func (da *DiffAnalyzer) analyzeRemovedCode(filePath string, hunk *DiffHunk, removedLines []string) *Change {
	code := strings.Join(removedLines, "\n")

	// Detect function removal
	if da.isFunctionRemoval(code) {
		return &Change{
			Type:        "function_removal",
			File:        filePath,
			Description: "Removed function",
			StartLine:   hunk.StartLine,
			EndLine:     hunk.EndLine,
			Confidence:  0.9,
			Details: map[string]interface{}{
				"functionName": da.extractFunctionName(code),
				"code":         code,
			},
		}
	}

	// Detect code removal
	return &Change{
		Type:        "code_removal",
		File:        filePath,
		Description: "Removed code",
		StartLine:   hunk.StartLine,
		EndLine:     hunk.EndLine,
		Confidence:  0.7,
		Details: map[string]interface{}{
			"code": code,
		},
	}
}

// analyzeModifiedCode analyzes modified code to detect patterns
func (da *DiffAnalyzer) analyzeModifiedCode(filePath string, hunk *DiffHunk, modifiedLines [][]string) *Change {
	if len(modifiedLines) == 0 {
		return nil
	}

	pair := modifiedLines[0]
	if len(pair) < 2 {
		return nil
	}

	oldCode := pair[0]
	newCode := pair[1]

	if change := da.detectVariableRenameChange(filePath, hunk, oldCode, newCode); change != nil {
		return change
	}
	if change := da.detectFunctionModificationChange(filePath, hunk, oldCode, newCode); change != nil {
		return change
	}

	return &Change{
		Type:        "code_modification",
		File:        filePath,
		Description: "Modified code",
		StartLine:   hunk.StartLine,
		EndLine:     hunk.EndLine,
		Confidence:  0.6,
		Details: map[string]interface{}{
			"oldCode": oldCode,
			"newCode": newCode,
		},
	}
}

// createInsertCodeOperation creates an insert_code operation
func (da *DiffAnalyzer) createInsertCodeOperation(change *Change) *orchestrator.RefactoringOperation {
	code, _ := change.Details["code"].(string)

	return &orchestrator.RefactoringOperation{
		Type:        "insert_code",
		Description: change.Description,
		File:        change.File,
		Target: &orchestrator.TargetSpecification{
			StartLine: &change.StartLine,
		},
		Parameters: map[string]interface{}{
			"codeSnippet": code,
			"location": map[string]interface{}{
				"type": "at_end",
			},
		},
		Fallback: &orchestrator.FallbackStrategy{
			Type:        "skip",
			Description: "Skip if target not found",
		},
	}
}

// createRenameVariableOperation creates a rename_variable operation
func (da *DiffAnalyzer) createRenameVariableOperation(change *Change) *orchestrator.RefactoringOperation {
	oldName, _ := change.Details["oldName"].(string)
	newName, _ := change.Details["newName"].(string)

	return &orchestrator.RefactoringOperation{
		Type:        "rename_variable",
		Description: change.Description,
		File:        change.File,
		Target: &orchestrator.TargetSpecification{
			StartLine: &change.StartLine,
			EndLine:   &change.EndLine,
		},
		Parameters: map[string]interface{}{
			"oldName": oldName,
			"newName": newName,
		},
	}
}

// createExtractMethodOperation creates an extract_method operation
func (da *DiffAnalyzer) createExtractMethodOperation(change *Change) *orchestrator.RefactoringOperation {
	functionName, _ := change.Details["functionName"].(string)

	return &orchestrator.RefactoringOperation{
		Type:        "extract_method",
		Description: "Extract modified code into separate method",
		File:        change.File,
		Target: &orchestrator.TargetSpecification{
			FunctionName: functionName,
		},
		Parameters: map[string]interface{}{
			"methodName": fmt.Sprintf("extracted_%s", functionName),
		},
		Fallback: &orchestrator.FallbackStrategy{
			Type:        "skip",
			Description: "Skip if function not found",
		},
	}
}

func (da *DiffAnalyzer) detectMethodAdditionChange(filePath string, hunk *DiffHunk, code string) *Change {
	if !da.isMethodAddition(code) {
		return nil
	}
	return &Change{
		Type:        "method_addition",
		File:        filePath,
		Description: "Added new method",
		StartLine:   hunk.StartLine,
		EndLine:     hunk.EndLine,
		Confidence:  0.9,
		Details: map[string]interface{}{
			"methodName":   da.extractMethodName(code),
			"receiverType": da.extractReceiverType(code),
			"code":         code,
		},
	}
}

func (da *DiffAnalyzer) detectFunctionAdditionChange(filePath string, hunk *DiffHunk, code string) *Change {
	if !da.isFunctionAddition(code) {
		return nil
	}
	return &Change{
		Type:        "function_addition",
		File:        filePath,
		Description: "Added new function",
		StartLine:   hunk.StartLine,
		EndLine:     hunk.EndLine,
		Confidence:  0.9,
		Details: map[string]interface{}{
			"functionName": da.extractFunctionName(code),
			"code":         code,
		},
	}
}

func (da *DiffAnalyzer) detectInterfaceAdditionChange(filePath string, hunk *DiffHunk, code string) *Change {
	if !da.isInterfaceAddition(code) {
		return nil
	}
	return &Change{
		Type:        "interface_addition",
		File:        filePath,
		Description: "Added new interface",
		StartLine:   hunk.StartLine,
		EndLine:     hunk.EndLine,
		Confidence:  0.9,
		Details: map[string]interface{}{
			"interfaceName": da.extractInterfaceName(code),
			"code":          code,
		},
	}
}

func (da *DiffAnalyzer) detectStructAdditionChange(filePath string, hunk *DiffHunk, code string) *Change {
	if !da.isStructAddition(code) {
		return nil
	}
	return &Change{
		Type:        "struct_addition",
		File:        filePath,
		Description: "Added new struct",
		StartLine:   hunk.StartLine,
		EndLine:     hunk.EndLine,
		Confidence:  0.9,
		Details: map[string]interface{}{
			"structName": da.extractStructName(code),
			"code":       code,
		},
	}
}

func (da *DiffAnalyzer) detectVariableRenameChange(filePath string, hunk *DiffHunk, oldCode, newCode string) *Change {
	if !da.isVariableRename(oldCode, newCode) {
		return nil
	}
	return &Change{
		Type:        "variable_rename",
		File:        filePath,
		Description: "Renamed variable",
		StartLine:   hunk.StartLine,
		EndLine:     hunk.EndLine,
		Confidence:  0.8,
		Details: map[string]interface{}{
			"oldName": da.extractVariableName(oldCode),
			"newName": da.extractVariableName(newCode),
			"oldCode": oldCode,
			"newCode": newCode,
		},
	}
}

func (da *DiffAnalyzer) detectFunctionModificationChange(filePath string, hunk *DiffHunk, oldCode, newCode string) *Change {
	if !da.isFunctionModification(oldCode, newCode) {
		return nil
	}
	return &Change{
		Type:        "function_modification",
		File:        filePath,
		Description: "Modified function",
		StartLine:   hunk.StartLine,
		EndLine:     hunk.EndLine,
		Confidence:  0.8,
		Details: map[string]interface{}{
			"functionName": da.extractFunctionName(oldCode),
			"oldCode":      oldCode,
			"newCode":      newCode,
		},
	}
}

// detectLanguage detects the programming language from file extension
func (da *DiffAnalyzer) detectLanguage(path string) string {
	if strings.HasSuffix(path, ".go") {
		return "go"
	}
	if strings.HasSuffix(path, ".js") || strings.HasSuffix(path, ".ts") {
		return "javascript"
	}
	if strings.HasSuffix(path, ".py") {
		return "python"
	}
	if strings.HasSuffix(path, ".java") {
		return "java"
	}
	return "unknown"
}
