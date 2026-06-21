package main

import (
	"fmt"
	"strings"

	"github.com/chrisophus/gorefactor/orchestrator"
)

var moveFlags = mutFlagSpec(nil)

func init() {
	registerCommand(Command{
		Name:        "move",
		Description: "Move a function or method to a different file",
		Usage:       "move <source-file> <Func|Receiver:Method> <destination-file> [--json] [--dry-run] [--gate]",
		MinArgs:     3,
		MaxArgs:     3,
		Flags:       moveFlags,
		Run:         moveCode,
	})
}

func moveCode(args []string) error {
	pos, flags := parseFlags(args, moveFlags)
	if len(pos) < 3 {
		return usageErrorf("usage: move <source-file> <target-name> <destination-file>\n\nExamples:\n  gorefactor move service.go GetUser utils.go\n  gorefactor move handler.go Handler:Process helpers.go")
	}

	sourceFile := pos[0]
	targetName := pos[1]
	destFile := pos[2]

	m := &mutation{op: "move", file: sourceFile, files: []string{sourceFile, destFile}}
	m.setCommonFlags(flags)

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

	// Verify the target exists in the source file before executing.
	normalized := functionName
	if receiverType != "" {
		normalized = strings.TrimPrefix(receiverType, "*") + ":" + functionName
	}
	if err := validateDeclTarget(sourceFile, normalized); err != nil {
		// Keep the original error for exit code preservation
		// It's a cliError with exitNotFound (code 2)
		return m.fail(err)
	}

	return m.run(func() (string, error) {
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

		orch := orchestrator.NewOrchestrator()
		orch.SkipSnapshot = true
		if err := orch.RegisterPlan(plan); err != nil {
			return "", fmt.Errorf("failed to register plan: %w", err)
		}

		result, err := orch.ExecutePlan(plan.Name)
		if err != nil {
			// Only wrap execution errors with DetailedError for non-critical errors
			errMsg := err.Error()
			if strings.Contains(errMsg, "import") || strings.Contains(errMsg, "circular") {
				return "", ExampleImportCycleError(sourceFile, destFile, targetName, []string{sourceFile, destFile})
			}
			// Return unwrapped for now (preserves error codes)
			return "", fmt.Errorf("move operation failed: %w", err)
		}
		if !result.Success {
			// Operation failed with specific error
			errMsg := strings.Join(result.Errors, "; ")
			if strings.Contains(errMsg, "import") || strings.Contains(errMsg, "circular") {
				return "", ExampleImportCycleError(sourceFile, destFile, targetName, []string{sourceFile, destFile})
			}
			// Return unwrapped for now (preserves error codes)
			return "", fmt.Errorf("move operation failed: %s", errMsg)
		}

		var b strings.Builder
		fmt.Fprintf(&b, "✓ Successfully moved %s to %s", targetName, destFile)
		for _, change := range result.Operations[0].Changes {
			fmt.Fprintf(&b, "\n  %s: %s (lines %d-%d)", change.Type, change.Description, change.StartLine, change.EndLine)
		}
		return b.String(), nil
	})
}
