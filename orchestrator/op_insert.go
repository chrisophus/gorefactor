package orchestrator

import (
	"fmt"
	"os"
	"strings"
)

func (o *Orchestrator) executeInsertCode(operation *RefactoringOperation, result *OperationResult) error {

	codeSnippet, ok := operation.Parameters["codeSnippet"].(string)
	if !ok {
		return fmt.Errorf("codeSnippet parameter is required for insert_code operation")
	}

	locationData, ok := operation.Parameters["location"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("location parameter is required for insert_code operation")
	}

	location := &InsertionLocation{
		Type: locationData["type"].(string),
	}
	if functionName, ok := locationData["functionName"].(string); ok {
		location.FunctionName = functionName
	}
	if methodName, ok := locationData["methodName"].(string); ok {
		location.MethodName = methodName
	}
	if receiverType, ok := locationData["receiverType"].(string); ok {
		location.ReceiverType = receiverType
	}
	if lineNumber, ok := locationData["lineNumber"].(float64); ok {
		location.LineNumber = int(lineNumber)
	}
	if codePattern, ok := locationData["codePattern"].(string); ok {
		location.CodePattern = codePattern
	}

	inserter := NewCodeInserter()
	insertionResult, err := inserter.InsertCode(operation.File, location, codeSnippet)
	if err != nil {
		return fmt.Errorf("failed to insert code: %w", err)
	}

	result.Changes = append(result.Changes, &CodeChange{
		Type:        "insert_code",
		File:        insertionResult.File,
		StartLine:   insertionResult.StartLine,
		EndLine:     insertionResult.EndLine,
		Description: insertionResult.Description,
		NewCode:     insertionResult.InsertedCode,
	})

	return nil
}

func (o *Orchestrator) executeCreateFile(operation *RefactoringOperation, result *OperationResult) error {
	codeSnippet, ok := operation.Parameters["codeSnippet"].(string)
	if !ok {
		return fmt.Errorf("codeSnippet parameter is required for create_file operation")
	}

	if _, err := os.Stat(operation.File); err == nil {

		if operation.Fallback != nil && operation.Fallback.Type == "skip" {
			result.Success = true
			result.Applied = false
			result.Message = "File already exists and fallback is skip"
			result.Changes = []*CodeChange{}
			return nil
		}
	}

	if err := os.WriteFile(operation.File, []byte(codeSnippet), 0644); err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}

	lines := strings.Split(codeSnippet, "\n")
	result.Changes = append(result.Changes, &CodeChange{
		Type:        "create_file",
		File:        operation.File,
		StartLine:   1,
		EndLine:     len(lines),
		Description: "Created new file",
		NewCode:     codeSnippet,
	})

	return nil
}

func (o *Orchestrator) executeReplaceCode(operation *RefactoringOperation, result *OperationResult) error {
	codePattern, _ := operation.Parameters["codePattern"].(string)
	if codePattern == "" {
		return fmt.Errorf("codePattern parameter is required for replace_code")
	}
	replacementCode, _ := operation.Parameters["replacementCode"].(string)
	if replacementCode == "" {
		return fmt.Errorf("replacementCode parameter is required for replace_code")
	}
	locationMap, ok := operation.Parameters["location"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("location parameter is required for replace_code")
	}
	funcName, _ := locationMap["functionName"].(string)
	methodName, _ := locationMap["methodName"].(string)
	receiverType, _ := locationMap["receiverType"].(string)
	ci := NewCodeInserter()
	ins, err := ci.ReplaceCodeBlock(operation.File, &InsertionLocation{
		FunctionName: funcName,
		MethodName:   methodName,
		ReceiverType: receiverType,
	}, codePattern, replacementCode)
	if err != nil {
		return err
	}
	result.Changes = append(result.Changes, &CodeChange{
		Type:        "replace_code",
		File:        operation.File,
		StartLine:   ins.StartLine,
		EndLine:     ins.EndLine,
		Description: ins.Description,
		NewCode:     ins.InsertedCode,
	})
	return nil
}
