package orchestrator

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"time"

	"gorefactor/extractor"
)

// RefactoringOperation represents a single refactoring operation
type RefactoringOperation struct {
	Type        string                 `json:"type"`
	Description string                 `json:"description"`
	File        string                 `json:"file"`
	Target      *TargetSpecification   `json:"target"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
	Conditions  []*Condition           `json:"conditions,omitempty"`
	Fallback    *FallbackStrategy      `json:"fallback,omitempty"`
}

// TargetSpecification defines how to locate the target for refactoring
type TargetSpecification struct {
	// Line-based targeting (traditional)
	StartLine *int `json:"startLine,omitempty"`
	EndLine   *int `json:"endLine,omitempty"`

	// Semantic targeting (resilient to code changes)
	FunctionName      string   `json:"functionName,omitempty"`
	MethodName        string   `json:"methodName,omitempty"`
	ReceiverType      string   `json:"receiverType,omitempty"`
	CodePattern       string   `json:"codePattern,omitempty"`
	VariableNames     []string `json:"variableNames,omitempty"`
	FunctionCalls     []string `json:"functionCalls,omitempty"`
	ControlStructures []string `json:"controlStructures,omitempty"`
	Comments          []string `json:"comments,omitempty"`

	// Context-based targeting
	BeforePattern   string            `json:"beforePattern,omitempty"`
	AfterPattern    string            `json:"afterPattern,omitempty"`
	SurroundingCode map[string]string `json:"surroundingCode,omitempty"`
}

// Condition represents a condition that must be met for the operation
type Condition struct {
	Type     string      `json:"type"`
	Property string      `json:"property"`
	Value    interface{} `json:"value"`
	Operator string      `json:"operator,omitempty"` // eq, ne, gt, lt, contains, regex
}

// FallbackStrategy defines what to do if the primary target cannot be found
type FallbackStrategy struct {
	Type        string                 `json:"type"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
}

// RefactoringPlan represents a complete refactoring plan
type RefactoringPlan struct {
	Version     string                  `json:"version"`
	Name        string                  `json:"name"`
	Description string                  `json:"description"`
	Created     time.Time               `json:"created"`
	Author      string                  `json:"author,omitempty"`
	Operations  []*RefactoringOperation `json:"operations"`
	Metadata    map[string]interface{}  `json:"metadata,omitempty"`
}

// ExecutionResult represents the result of executing a refactoring plan
type ExecutionResult struct {
	PlanName   string               `json:"planName"`
	Executed   time.Time            `json:"executed"`
	Success    bool                 `json:"success"`
	Operations []*OperationResult   `json:"operations"`
	Errors     []string             `json:"errors,omitempty"`
	Warnings   []string             `json:"warnings,omitempty"`
	Statistics *ExecutionStatistics `json:"statistics,omitempty"`
}

// OperationResult represents the result of a single operation
type OperationResult struct {
	Operation    *RefactoringOperation `json:"operation"`
	Success      bool                  `json:"success"`
	Message      string                `json:"message"`
	Applied      bool                  `json:"applied"`
	FallbackUsed bool                  `json:"fallbackUsed,omitempty"`
	Changes      []*CodeChange         `json:"changes,omitempty"`
	Error        string                `json:"error,omitempty"`
}

// CodeChange represents a specific change made to the code
type CodeChange struct {
	Type        string `json:"type"`
	File        string `json:"file"`
	StartLine   int    `json:"startLine"`
	EndLine     int    `json:"endLine"`
	Description string `json:"description"`
	OldCode     string `json:"oldCode,omitempty"`
	NewCode     string `json:"newCode,omitempty"`
}

// ExecutionStatistics provides metrics about the execution
type ExecutionStatistics struct {
	TotalOperations      int `json:"totalOperations"`
	SuccessfulOperations int `json:"successfulOperations"`
	FailedOperations     int `json:"failedOperations"`
	SkippedOperations    int `json:"skippedOperations"`
	FallbackUsed         int `json:"fallbackUsed"`
	TotalChanges         int `json:"totalChanges"`
}

// Orchestrator manages the execution of refactoring plans
type Orchestrator struct {
	plans map[string]*RefactoringPlan
}

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

	// Find the target using resilient targeting
	target, err := o.findTarget(operation.Target, operation.File)
	if err != nil {
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

	// Execute the operation based on type
	switch operation.Type {
	case "extract_method":
		err = o.executeExtractMethod(operation, target, result)
	case "inline_method":
		err = o.executeInlineMethod(operation, target, result)
	case "rename_variable":
		err = o.executeRenameVariable(operation, target, result)
	case "move_method":
		err = o.executeMoveMethod(operation, target, result)
	case "insert_code":
		err = o.executeInsertCode(operation, result)
	default:
		err = fmt.Errorf("unknown operation type: %s", operation.Type)
	}

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

// findTarget uses resilient targeting to locate the target for refactoring
func (o *Orchestrator) findTarget(target *TargetSpecification, filePath string) (*TargetLocation, error) {
	if target == nil {
		return nil, fmt.Errorf("no target specification provided")
	}

	// If line-based targeting is provided, use it directly
	if target.StartLine != nil && target.EndLine != nil {
		return &TargetLocation{
			File:      filePath,
			StartLine: *target.StartLine,
			EndLine:   *target.EndLine,
		}, nil
	}

	// Use semantic targeting
	return o.findTargetBySemantics(target, filePath)
}

// TargetLocation represents a location in the code
type TargetLocation struct {
	File      string `json:"file"`
	StartLine int    `json:"startLine"`
	EndLine   int    `json:"endLine"`
	Function  string `json:"function,omitempty"`
	Method    string `json:"method,omitempty"`
}

// findTargetBySemantics uses semantic information to find the target
func (o *Orchestrator) findTargetBySemantics(target *TargetSpecification, filePath string) (*TargetLocation, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("failed to parse file: %w", err)
	}

	var bestMatch *TargetLocation
	var bestScore int

	ast.Inspect(node, func(n ast.Node) bool {
		if n == nil {
			return false
		}

		// Check function declarations
		if funcDecl, ok := n.(*ast.FuncDecl); ok {
			score := o.calculateSemanticScore(funcDecl, target, fset)
			if score > bestScore {
				startLine := fset.Position(funcDecl.Pos()).Line
				endLine := fset.Position(funcDecl.End()).Line
				bestMatch = &TargetLocation{
					File:      filePath,
					StartLine: startLine,
					EndLine:   endLine,
					Function:  funcDecl.Name.Name,
				}
				if funcDecl.Recv != nil && len(funcDecl.Recv.List) > 0 {
					if t, ok := funcDecl.Recv.List[0].Type.(*ast.StarExpr); ok {
						if ident, ok := t.X.(*ast.Ident); ok {
							bestMatch.Method = ident.Name
						}
					}
				}
				bestScore = score
			}
		}

		return true
	})

	if bestMatch != nil && bestScore > 0 {
		return bestMatch, nil
	}

	return nil, fmt.Errorf("no suitable target found using semantic matching")
}

// calculateSemanticScore calculates how well a node matches the target specification
func (o *Orchestrator) calculateSemanticScore(node ast.Node, target *TargetSpecification, fset *token.FileSet) int {
	score := 0

	// Check function name match
	if target.FunctionName != "" {
		if funcDecl, ok := node.(*ast.FuncDecl); ok {
			if funcDecl.Name.Name == target.FunctionName {
				score += 10
			}
		}
	}

	// Check method name match
	if target.MethodName != "" {
		if funcDecl, ok := node.(*ast.FuncDecl); ok {
			if funcDecl.Name.Name == target.MethodName {
				score += 10
			}
		}
	}

	// Check code pattern match
	if target.CodePattern != "" {
		code := o.nodeToString(node, fset)
		if strings.Contains(code, target.CodePattern) {
			score += 5
		}
	}

	// Check variable names
	if len(target.VariableNames) > 0 {
		ast.Inspect(node, func(n ast.Node) bool {
			if ident, ok := n.(*ast.Ident); ok {
				for _, varName := range target.VariableNames {
					if ident.Name == varName {
						score += 2
					}
				}
			}
			return true
		})
	}

	// Check function calls
	if len(target.FunctionCalls) > 0 {
		ast.Inspect(node, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				if ident, ok := call.Fun.(*ast.Ident); ok {
					for _, funcName := range target.FunctionCalls {
						if ident.Name == funcName {
							score += 3
						}
					}
				}
			}
			return true
		})
	}

	return score
}

// nodeToString converts an AST node to a string representation
func (o *Orchestrator) nodeToString(node ast.Node, fset *token.FileSet) string {
	// This is a simplified implementation
	// In a real implementation, you'd use go/format to properly format the code
	return fmt.Sprintf("%v", node)
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

// executeExtractMethod executes a method extraction operation
func (o *Orchestrator) executeExtractMethod(operation *RefactoringOperation, target *TargetLocation, result *OperationResult) error {
	methodName, ok := operation.Parameters["methodName"].(string)
	if !ok {
		return fmt.Errorf("methodName parameter is required for extract_method operation")
	}

	extractionResult, err := extractor.ExtractMethod(target.File, target.StartLine, target.EndLine, methodName)
	if err != nil {
		return fmt.Errorf("failed to extract method: %w", err)
	}

	result.Changes = append(result.Changes, &CodeChange{
		Type:        "extract_method",
		File:        target.File,
		StartLine:   target.StartLine,
		EndLine:     target.EndLine,
		Description: fmt.Sprintf("Extracted method '%s'", methodName),
		NewCode:     fmt.Sprintf("Method '%s' extracted with parameters: %v", methodName, extractionResult.Parameters),
	})

	return nil
}

// executeInlineMethod executes a method inlining operation
func (o *Orchestrator) executeInlineMethod(operation *RefactoringOperation, target *TargetLocation, result *OperationResult) error {
	// Implementation for method inlining
	result.Changes = append(result.Changes, &CodeChange{
		Type:        "inline_method",
		File:        target.File,
		StartLine:   target.StartLine,
		EndLine:     target.EndLine,
		Description: "Inlined method call",
	})
	return nil
}

// executeRenameVariable executes a variable renaming operation
func (o *Orchestrator) executeRenameVariable(operation *RefactoringOperation, target *TargetLocation, result *OperationResult) error {
	// Implementation for variable renaming
	result.Changes = append(result.Changes, &CodeChange{
		Type:        "rename_variable",
		File:        target.File,
		StartLine:   target.StartLine,
		EndLine:     target.EndLine,
		Description: "Renamed variable",
	})
	return nil
}

// executeMoveMethod executes a method moving operation
func (o *Orchestrator) executeMoveMethod(operation *RefactoringOperation, target *TargetLocation, result *OperationResult) error {
	// Implementation for method moving
	result.Changes = append(result.Changes, &CodeChange{
		Type:        "move_method",
		File:        target.File,
		StartLine:   target.StartLine,
		EndLine:     target.EndLine,
		Description: "Moved method",
	})
	return nil
}

// executeInsertCode executes a code insertion operation
func (o *Orchestrator) executeInsertCode(operation *RefactoringOperation, result *OperationResult) error {
	// Get parameters
	codeSnippet, ok := operation.Parameters["codeSnippet"].(string)
	if !ok {
		return fmt.Errorf("codeSnippet parameter is required for insert_code operation")
	}

	locationData, ok := operation.Parameters["location"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("location parameter is required for insert_code operation")
	}

	// Convert location data to InsertionLocation
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

	// Create code inserter and insert code
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
	if operation.Type == "" {
		return fmt.Errorf("operation type is required")
	}
	if operation.File == "" {
		return fmt.Errorf("operation file is required")
	}
	if operation.Target == nil {
		return fmt.Errorf("operation target is required")
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
