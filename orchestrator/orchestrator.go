package orchestrator

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"time"
)

// NewOrchestrator creates a new orchestrator instance
func NewOrchestrator() *Orchestrator {
	return &Orchestrator{
		plans: make(map[string]*RefactoringPlan),
	}
}

// LoadPlan loads a refactoring plan from a JSON file
func (o *Orchestrator) LoadPlan(filePath string) (*RefactoringPlan, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read plan file: %w", err)
	}

	var plan RefactoringPlan
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, fmt.Errorf("failed to parse plan JSON: %w", err)
	}

	// Validate the plan
	if err := o.validatePlan(&plan); err != nil {
		return nil, fmt.Errorf("invalid plan: %w", err)
	}

	o.plans[plan.Name] = &plan
	return &plan, nil
}

// ExecutePlan executes a refactoring plan
func (o *Orchestrator) ExecutePlan(planName string) (*ExecutionResult, error) {
	if p, ok := o.plans[planName]; ok {
		if err := o.createSnapshot(p, SnapshotDir(planName)); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: snapshot failed: %v\n", err)
		}
	}

	plan, exists := o.plans[planName]
	if !exists {
		return nil, fmt.Errorf("plan '%s' not found", planName)
	}

	result := &ExecutionResult{
		PlanName:   planName,
		Executed:   time.Now(),
		Operations: make([]*OperationResult, 0, len(plan.Operations)),
		Statistics: &ExecutionStatistics{},
	}

	for i, operation := range plan.Operations {
		opResult := o.executeOperation(operation)
		result.Operations = append(result.Operations, opResult)

		// Update statistics
		result.Statistics.TotalOperations++
		if opResult.Success {
			result.Statistics.SuccessfulOperations++
		} else {
			result.Statistics.FailedOperations++
			result.Errors = append(result.Errors, fmt.Sprintf("Operation %d: %s", i+1, opResult.Error))
		}
		if opResult.FallbackUsed {
			result.Statistics.FallbackUsed++
		}
		result.Statistics.TotalChanges += len(opResult.Changes)
	}

	result.Success = len(result.Errors) == 0
	return result, nil
}

// executeOperation executes a single refactoring operation
func (o *Orchestrator) executeOperation(operation *RefactoringOperation) *OperationResult {
	result := &OperationResult{
		Operation: operation,
		Changes:   make([]*CodeChange, 0),
	}

	// Check conditions first
	if !o.checkConditions(operation.Conditions) {
		result.Success = false
		result.Applied = false
		result.Message = "Conditions not met"
		return result
	}

	// Special handling for insert_code with at_beginning on new files
	if operation.Type == "insert_code" {
		locationData, ok := operation.Parameters["location"].(map[string]interface{})
		if ok {
			locationType, _ := locationData["type"].(string)
			if locationType == "at_beginning" {
				// Check if file exists
				if _, err := os.Stat(operation.File); os.IsNotExist(err) {
					// Skip target finding for new file creation
					err = o.executeInsertCode(operation, result)
					if err != nil {
						result.Success = false
						result.Error = err.Error()
					} else {
						result.Success = true
						result.Applied = true
						result.Message = "Operation completed successfully"
					}
					return result
				}
			}
		}
	}

	// Find the target using resilient targeting
	// Note: insert_code operations may not need a target, but we'll still try to find one if specified
	var target *TargetLocation
	var err error
	if operation.Target != nil {
		target, err = o.findTarget(operation.Target, operation.File)
		if err != nil {
			// For insert_code, target is optional
			if operation.Type != "insert_code" {
				// Try fallback strategy
				if operation.Fallback != nil {
					target, err = o.executeFallback(operation.Fallback, operation.File)
					if err != nil {
						result.Success = false
						result.Error = fmt.Sprintf("Failed to find target and fallback: %v", err)
						return result
					}
					result.FallbackUsed = true
				} else {
					result.Success = false
					result.Error = fmt.Sprintf("Failed to find target: %v", err)
					return result
				}
			}
			// For insert_code, we can proceed without a target
		}
	}

	// Execute the operation based on type
	switch operation.Type {
	case "move_method":
		err = o.executeMoveMethod(operation, target, result)
	case "insert_code":
		err = o.executeInsertCode(operation, result)
	case "create_file":
		err = o.executeCreateFile(operation, result)
	case "remove_code_block":
		err = o.executeRemoveCodeBlock(operation, result)
	case "replace_code":
		err = o.executeReplaceCode(operation, result)
	default:
		err = fmt.Errorf("unknown operation type: %s", operation.Type)
	}

	if err != nil {
		result.Success = false
		result.Error = err.Error()
	} else {
		result.Success = true
		// Only set Applied to true if it hasn't been explicitly set to false
		// (e.g., by create_file with skip fallback)
		if !result.Applied && result.Message == "" {
			result.Applied = true
		}
		if result.Message == "" {
			result.Message = "Operation completed successfully"
		}
	}

	return result
}
func (o *Orchestrator) ExecuteOperations(ops []*RefactoringOperation) (*ExecutionResult, error) {
	plan := &RefactoringPlan{
		Version:    "1.0",
		Name:       "_exec",
		Operations: ops,
	}
	o.plans["_exec"] = plan
	return o.ExecutePlan("_exec")
}

// checkConditions verifies that all conditions are met
func (o *Orchestrator) checkConditions(conditions []*Condition) bool {
	if len(conditions) == 0 {
		return true
	}

	for _, condition := range conditions {
		if !o.evaluateCondition(condition) {
			return false
		}
	}
	return true
}

// evaluateCondition evaluates a single condition
func (o *Orchestrator) evaluateCondition(condition *Condition) bool {
	// This is a simplified implementation
	// In a real implementation, you'd evaluate the condition based on the current code state
	return true
}

// executeFallback executes a fallback strategy
func (o *Orchestrator) executeFallback(fallback *FallbackStrategy, filePath string) (*TargetLocation, error) {
	switch fallback.Type {
	case "skip":
		return nil, fmt.Errorf("fallback strategy: skip")
	case "use_default":
		// Use a default target (e.g., first function in file)
		return o.findDefaultTarget(filePath)
	default:
		return nil, fmt.Errorf("unknown fallback strategy: %s", fallback.Type)
	}
}

// findDefaultTarget finds a default target when fallback is needed
func (o *Orchestrator) findDefaultTarget(filePath string) (*TargetLocation, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	var firstFunc *ast.FuncDecl
	ast.Inspect(node, func(n ast.Node) bool {
		if firstFunc == nil {
			if funcDecl, ok := n.(*ast.FuncDecl); ok {
				firstFunc = funcDecl
				return false
			}
		}
		return true
	})

	if firstFunc != nil {
		startLine := fset.Position(firstFunc.Pos()).Line
		endLine := fset.Position(firstFunc.End()).Line
		return &TargetLocation{
			File:      filePath,
			StartLine: startLine,
			EndLine:   endLine,
			Function:  firstFunc.Name.Name,
		}, nil
	}

	return nil, fmt.Errorf("no functions found in file")
}

func (o *Orchestrator) executeRemoveCodeBlock(operation *RefactoringOperation, result *OperationResult) error {
	codePattern, _ := operation.Parameters["codePattern"].(string)
	if codePattern == "" {
		return fmt.Errorf("codePattern parameter is required for remove_code_block")
	}
	locationMap, ok := operation.Parameters["location"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("location parameter is required for remove_code_block")
	}
	funcName, _ := locationMap["functionName"].(string)
	methodName, _ := locationMap["methodName"].(string)
	receiverType, _ := locationMap["receiverType"].(string)
	ci := NewCodeInserter()
	ins, err := ci.RemoveCodeBlock(operation.File, &InsertionLocation{
		FunctionName: funcName,
		MethodName:   methodName,
		ReceiverType: receiverType,
	}, codePattern)
	if err != nil {
		return err
	}
	result.Changes = append(result.Changes, &CodeChange{
		Type:        "remove_code_block",
		File:        operation.File,
		StartLine:   ins.StartLine,
		EndLine:     ins.EndLine,
		Description: ins.Description,
	})
	return nil
}

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
