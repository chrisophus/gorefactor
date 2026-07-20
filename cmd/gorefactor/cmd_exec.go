package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/chrisophus/gorefactor/orchestrator"
)

func execOperation(args []string) error {
	var data []byte
	var err error

	if len(args) == 0 || args[0] == "-" || args[0] == "-stdin" {
		data, err = io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("failed to read stdin: %w", err)
		}
	} else {
		data = []byte(args[0])
	}

	trimmed := bytes.TrimSpace(data)
	var ops []*orchestrator.RefactoringOperation
	if len(trimmed) > 0 && trimmed[0] == '[' {
		if err := json.Unmarshal(data, &ops); err != nil {
			return fmt.Errorf("failed to parse operations: %w", err)
		}
	} else {
		var op orchestrator.RefactoringOperation
		if err := json.Unmarshal(data, &op); err != nil {
			return fmt.Errorf("failed to parse operation: %w", err)
		}
		ops = []*orchestrator.RefactoringOperation{&op}
	}

	orch := orchestrator.NewOrchestrator()
	result, err := orch.ExecuteOperations(ops)
	if err != nil {
		return fmt.Errorf("execute operations: %w", err)
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if encErr := encoder.Encode(result); encErr != nil {
		return encErr
	}
	if !result.Success {
		return fmt.Errorf("one or more operations failed")
	}
	return nil
}
