package orchestrator

import (
	"encoding/json"
	"fmt"
	"os"
)

// SaveResult saves an execution result to a JSON file
func (o *Orchestrator) SaveResult(result *ExecutionResult, filePath string) error {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal result: %w", err)
	}

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write result file: %w", err)
	}

	return nil
}

func (o *Orchestrator) isNewFileAtBeginning(operation *RefactoringOperation) bool {
	if operation.Type != "insert_code" {
		return false
	}
	locationData, ok := operation.Parameters["location"].(map[string]interface{})
	if !ok {
		return false
	}
	locationType, _ := locationData["type"].(string)
	if locationType != "at_beginning" {
		return false
	}
	_, err := os.Stat(operation.File)
	return os.IsNotExist(err)
}

func (o *Orchestrator) finalizeResult(result *OperationResult, err error) *OperationResult {
	if err != nil {
		result.Success = false
		result.Error = err.Error()
		return result
	}
	result.Success = true
	if !result.Applied && result.Message == "" {
		result.Applied = true
	}
	if result.Message == "" {
		result.Message = "Operation completed successfully"
	}
	return result
}
