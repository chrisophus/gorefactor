package orchestrator

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// NewOrchestrator creates a new orchestrator instance
func NewOrchestrator() *Orchestrator {
	return &Orchestrator{
		plans: make(map[string]*RefactoringPlan),
	}
}

// RegisterPlan registers a refactoring plan for execution
func (o *Orchestrator) RegisterPlan(plan *RefactoringPlan) error {
	if err := o.validatePlan(plan); err != nil {
		return fmt.Errorf("invalid plan: %w", err)
	}
	o.plans[plan.Name] = plan
	return nil
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
	if p, ok := o.plans[planName]; ok && !o.SkipSnapshot {
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

// Check conditions first

// Special handling for insert_code with at_beginning on new files

// Check if file exists

// Skip target finding for new file creation

// Find the target using resilient targeting
// Note: insert_code operations may not need a target, but we'll still try to find one if specified

// For insert_code and rename_declaration, target is optional

// Try fallback strategy

// For insert_code, we can proceed without a target

// Execute the operation based on type

// Only set Applied to true if it hasn't been explicitly set to false
// (e.g., by create_file with skip fallback)

func (o *Orchestrator) ExecuteOperations(ops []*RefactoringOperation) (*ExecutionResult, error) {
	plan := &RefactoringPlan{
		Version:    "1.0",
		Name:       "_exec",
		Operations: ops,
	}
	o.plans["_exec"] = plan
	return o.ExecutePlan("_exec")
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

func (o *Orchestrator) dispatchOperation(operation *RefactoringOperation, target *TargetLocation, result *OperationResult) error {
	switch operation.Type {
	case "move_method":
		return o.executeMoveMethod(operation, target, result)
	case "insert_code":
		return o.executeInsertCode(operation, result)
	case "create_file":
		return o.executeCreateFile(operation, result)
	case "remove_code_block":
		return o.executeRemoveCodeBlock(operation, result)
	case "replace_code":
		return o.executeReplaceCode(operation, result)
	case "delete_declaration":
		return o.executeDeleteDeclaration(operation, target, result)
	case "rename_declaration":
		return o.executeRenameDeclaration(operation, result)
	default:
		return fmt.Errorf("unknown operation type: %s", operation.Type)
	}
}

func (o *Orchestrator) executeOperation(operation *RefactoringOperation) *OperationResult {
	result := &OperationResult{
		Operation: operation,
		Changes:   make([]*CodeChange, 0),
	}
	if !o.checkConditions(operation.Conditions) {
		result.Success = false
		result.Applied = false
		result.Message = "Conditions not met"
		return result
	}
	if o.isNewFileAtBeginning(operation) {
		return o.finalizeResult(result, o.executeInsertCode(operation, result))
	}
	target, fallbackUsed, err := o.resolveTarget(operation)
	if err != nil {
		result.Success = false
		result.Error = err.Error()
		return result
	}
	if fallbackUsed {
		result.FallbackUsed = true
	}
	return o.finalizeResult(result, o.dispatchOperation(operation, target, result))
}
func (o *Orchestrator) resolveTarget(operation *RefactoringOperation) (*TargetLocation, bool, error) {
	if operation.Target == nil {
		return nil, false, nil
	}
	target, err := o.findTarget(operation.Target, operation.File)
	if err == nil {
		return target, false, nil
	}
	if operation.Type == "insert_code" || operation.Type == "rename_declaration" {
		return nil, false, nil
	}
	if operation.Fallback != nil {
		target, err = o.executeFallback(operation.Fallback, operation.File)
		if err != nil {
			return nil, false, fmt.Errorf("failed to find target and fallback: %v", err)
		}
		return target, true, nil
	}
	return nil, false, fmt.Errorf("failed to find target: %v", err)
}
