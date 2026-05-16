package main

import (
	"fmt"
	"gorefactor/orchestrator"
	"strings"
)

func moveCode(args []string) error {
	if len(args) < 3 {
		return fmt.Errorf("usage: move <source-file> <target-name> <destination-file>\n\nExamples:\n  gorefactor move service.go GetUser utils.go\n  gorefactor move handler.go Handler:Process helpers.go")
	}

	sourceFile := args[0]
	targetName := args[1]
	destFile := args[2]

	// Parse target name - could be "FunctionName" or "Receiver:MethodName"
	var functionName, receiverType string
	if strings.Contains(targetName, ":") {
		parts := strings.Split(targetName, ":")
		receiverType = parts[0]
		functionName = parts[1]
	} else if strings.Contains(targetName, ".") {
		parts := strings.Split(targetName, ".")
		receiverType = parts[0]
		functionName = parts[1]
	} else {
		functionName = targetName
	}

	// Create a refactoring plan and execute it
	plan := &orchestrator.RefactoringPlan{
		Version:     "1.0",
		Name:        "move_operation",
		Description: fmt.Sprintf("Move %s to %s", targetName, destFile),
		Operations: []*orchestrator.RefactoringOperation{
			{
				Type:        "move_method",
				Description: fmt.Sprintf("Move %s from %s to %s", targetName, sourceFile, destFile),
				File:        sourceFile,
				Target: &orchestrator.TargetSpecification{
					FunctionName: functionName,
					MethodName:   functionName,
					ReceiverType: receiverType,
				},
				Parameters: map[string]interface{}{
					"newFile": destFile,
				},
			},
		},
	}

	// Execute the plan
	orch := orchestrator.NewOrchestrator()
	orch.RegisterPlan(plan)

	result, err := orch.ExecutePlan(plan.Name)
	if err != nil {
		return fmt.Errorf("failed to move code: %w", err)
	}

	// Output results
	if result.Success {
		fmt.Printf("✓ Successfully moved %s to %s\n", targetName, destFile)
		for _, change := range result.Operations[0].Changes {
			fmt.Printf("  %s: %s (lines %d-%d)\n", change.Type, change.Description, change.StartLine, change.EndLine)
		}
	} else {
		fmt.Printf("✗ Failed to move %s\n", targetName)
		for _, err := range result.Errors {
			fmt.Printf("  Error: %s\n", err)
		}
		return fmt.Errorf("move operation failed")
	}

	return nil
}
