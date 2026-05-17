package orchestrator

import "fmt"

// validatePlan validates a refactoring plan
func (o *Orchestrator) validatePlan(plan *RefactoringPlan) error {
	if plan.Name == "" {
		return fmt.Errorf("plan name is required")
	}
	if len(plan.Operations) == 0 {
		return fmt.Errorf("plan must contain at least one operation")
	}

	for i, operation := range plan.Operations {
		if err := o.validateOperation(operation); err != nil {
			return fmt.Errorf("operation %d: %w", i+1, err)
		}
	}

	return nil
}

// validateOperation validates a single operation
func (o *Orchestrator) validateOperation(operation *RefactoringOperation) error {
	if operation.Type == "remove_code_block" || operation.Type == "replace_code" {
		if operation.File == "" {
			return fmt.Errorf("operation file is required")
		}
		return nil
	}

	if operation.Type == "" {
		return fmt.Errorf("operation type is required")
	}
	if operation.File == "" {
		return fmt.Errorf("operation file is required")
	}
	// Target is optional for insert_code and create_file operations
	if operation.Target == nil && operation.Type != "insert_code" && operation.Type != "create_file" {
		return fmt.Errorf("operation target is required for operation type: %s", operation.Type)
	}

	return nil
}
