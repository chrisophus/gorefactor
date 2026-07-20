package orchestrator

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

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

// ExecutePlan executes a refactoring plan. It never snapshots on its own: the
// mutation journal (RecordOperation / UndoLast) is the single undo system, and
// callers that want a plan run to be undoable journal it themselves (see
// orchestrate's journalPlanRun and the CLI mutation runner).
func (o *Orchestrator) ExecutePlan(planName string) (*ExecutionResult, error) {
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
		// Auto-detect cross-package move: when newFile is in a different package,
		// route to the cross-package handler which rewrites call sites and imports.
		newFile, _ := operation.Parameters["newFile"].(string)
		if newFile != "" && isCrossPackageMove(operation.File, newFile) {
			return o.executeCrossPackageMove(operation, target, result)
		}
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
		if h, ok := externalHandlers[operation.Type]; ok {
			changes, err := h(operation, target)
			if err != nil {
				return err
			}
			result.Changes = append(result.Changes, changes...)
			return nil
		}
		return fmt.Errorf("unknown operation type: %s", operation.Type)
	}
}

// executeCrossPackageMove routes a move_method / move_function operation to
// the cross-package handler when the destination is in a different package.
// It fails loudly (listing affected call sites) when the move would break
// the build — unexported refs, unexported function with callers, import cycle.
func (o *Orchestrator) executeCrossPackageMove(operation *RefactoringOperation, target *TargetLocation, result *OperationResult) error {
	newFile, _ := operation.Parameters["newFile"].(string)
	if newFile == "" {
		return fmt.Errorf("newFile parameter is required for cross-package move")
	}

	// Determine function name from the resolved target or operation target spec.
	funcName := ""
	if target != nil && target.Function != "" {
		funcName = target.Function
	} else if target != nil && target.Method != "" {
		funcName = target.Method
	} else if operation.Target != nil {
		if operation.Target.FunctionName != "" {
			funcName = operation.Target.FunctionName
		} else if operation.Target.MethodName != "" {
			funcName = operation.Target.MethodName
		}
	}
	if funcName == "" {
		return fmt.Errorf("cross-package move: cannot determine function name from target (set functionName or methodName in the target spec)")
	}

	h := NewCrossPackageOperationHandler()
	if err := h.MoveAcrossPackages(operation.File, newFile, funcName); err != nil {
		// MoveAcrossPackages already produces a detailed error message with
		// the call-site list when callers would break.
		return fmt.Errorf("cross-package move of %s from %s to %s failed: %w", funcName, operation.File, newFile, err)
	}

	result.Changes = append(result.Changes, &CodeChange{
		Type:        "move_method",
		File:        operation.File,
		Description: fmt.Sprintf("Moved %s to %s (cross-package)", funcName, newFile),
	})
	result.Changes = append(result.Changes, &CodeChange{
		Type:        "move_method",
		File:        newFile,
		Description: fmt.Sprintf("Added %s from %s (cross-package)", funcName, operation.File),
	})
	return nil
}

func (o *Orchestrator) executeOperation(operation *RefactoringOperation) *OperationResult {
	result := &OperationResult{
		Operation: operation,
		Changes:   make([]*CodeChange, 0),
	}
	conditionsMet, condErr := o.checkConditions(operation)
	if condErr != nil {
		result.Success = false
		result.Applied = false
		result.Error = fmt.Sprintf("condition evaluation failed: %v", condErr)
		return result
	}
	if !conditionsMet {
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
		return o.finalizeResult(result, err)
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
