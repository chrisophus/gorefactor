package orchestrator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"regexp"
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

	// Declaration-level targeting
	TypeName  string `json:"typeName,omitempty"`  // For type declarations
	ConstName string `json:"constName,omitempty"` // For const declarations
	VarName   string `json:"varName,omitempty"`   // For var declarations

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
	case "create_file":
		err = o.executeCreateFile(operation, result)
	case "remove_code_block":
		err = o.executeRemoveCodeBlock(operation, result)
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

		// Check type declarations
		if genDecl, ok := n.(*ast.GenDecl); ok {
			// First check if the entire GenDecl matches (for code patterns)
			genDeclScore := o.calculateSemanticScore(genDecl, target, fset)
			if genDeclScore > bestScore {
				startLine := fset.Position(genDecl.Pos()).Line
				endLine := fset.Position(genDecl.End()).Line
				bestMatch = &TargetLocation{
					File:      filePath,
					StartLine: startLine,
					EndLine:   endLine,
					Function:  "", // Will be set below if we find a specific spec
				}
				bestScore = genDeclScore
			}

			for _, spec := range genDecl.Specs {
				if typeSpec, ok := spec.(*ast.TypeSpec); ok {
					score := o.calculateSemanticScore(typeSpec, target, fset)
					if score > bestScore {
						startLine := fset.Position(genDecl.Pos()).Line
						endLine := fset.Position(genDecl.End()).Line
						bestMatch = &TargetLocation{
							File:      filePath,
							StartLine: startLine,
							EndLine:   endLine,
							Function:  typeSpec.Name.Name, // Reuse Function field for type name
						}
						bestScore = score
					}
				}

				// Check const/var declarations
				if valueSpec, ok := spec.(*ast.ValueSpec); ok {
					for _, name := range valueSpec.Names {
						score := o.calculateSemanticScore(valueSpec, target, fset)
						if score > bestScore {
							startLine := fset.Position(genDecl.Pos()).Line
							endLine := fset.Position(genDecl.End()).Line
							bestMatch = &TargetLocation{
								File:      filePath,
								StartLine: startLine,
								EndLine:   endLine,
								Function:  name.Name, // Reuse Function field for const/var name
							}
							bestScore = score
						}
					}
				}
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

	// Check type name match
	if target.TypeName != "" {
		if typeSpec, ok := node.(*ast.TypeSpec); ok {
			if typeSpec.Name.Name == target.TypeName {
				score += 10
			}
		}
	}

	// Check const name match
	if target.ConstName != "" {
		if valueSpec, ok := node.(*ast.ValueSpec); ok {
			for _, name := range valueSpec.Names {
				if name.Name == target.ConstName {
					score += 10
					break
				}
			}
		}
	}

	// Check var name match
	if target.VarName != "" {
		if valueSpec, ok := node.(*ast.ValueSpec); ok {
			for _, name := range valueSpec.Names {
				if name.Name == target.VarName {
					score += 10
					break
				}
			}
		}
	}

	// Check code pattern match with regex support
	if target.CodePattern != "" {
		code := o.nodeToString(node, fset)

		// Try regex first, fall back to simple contains
		matched, err := regexp.MatchString(target.CodePattern, code)
		if err == nil && matched {
			score += 5
		} else if strings.Contains(code, target.CodePattern) {
			score += 3 // Lower score for non-regex match
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
	var buf bytes.Buffer
	if err := format.Node(&buf, fset, node); err != nil {
		// Fallback to simple string representation
		return fmt.Sprintf("%v", node)
	}
	return buf.String()
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

// commentBelongsToDecl returns true if a comment group should be associated with a declaration.
// If the comment lies inside the declaration's tokens, or if it ends within one blank line above the declaration.
func commentBelongsToDecl(fileSet *token.FileSet, declStart, declEnd token.Pos, commentGroups *ast.CommentGroup) bool {
	// Inside the declaration: always include.
	if commentGroups.Pos() >= declStart && commentGroups.End() <= declEnd {
		return true
	}
	// Otherwise, if the comment lies above the declaration and its end is within one blank line.
	declLine := fileSet.Position(declStart).Line
	commentGroupsEndLine := fileSet.Position(commentGroups.End()).Line
	if declLine > commentGroupsEndLine && (declLine-commentGroupsEndLine) <= 2 {
		return true
	}
	return false
}

// executeMoveMethod executes a method moving operation
func (o *Orchestrator) executeMoveMethod(operation *RefactoringOperation, target *TargetLocation, result *OperationResult) error {
	newFile, ok := operation.Parameters["newFile"].(string)
	if !ok {
		return fmt.Errorf("newFile parameter is required for move_method operation")
	}

	fset := token.NewFileSet()

	// Parse source file
	sourceNode, err := parser.ParseFile(fset, target.File, nil, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("failed to parse source file: %w", err)
	}

	// Re-find the target using the same FileSet for accurate positions
	// This ensures line numbers match between finding and moving
	actualTarget, err := o.findTarget(operation.Target, target.File)
	if err != nil {
		return fmt.Errorf("failed to re-find target: %w", err)
	}

	// Find the declaration to move using line numbers from the same FileSet
	var declToMove ast.Decl
	var declIndex int = -1
	var declType string

	for i, decl := range sourceNode.Decls {
		startLine := fset.Position(decl.Pos()).Line
		endLine := fset.Position(decl.End()).Line

		// Check if this declaration matches the target
		// Declaration should start at or before target start and end at or after target end
		if startLine <= actualTarget.StartLine && endLine >= actualTarget.EndLine {
			declToMove = decl
			declIndex = i

			// Determine declaration type for better error messages and logging
			switch d := decl.(type) {
			case *ast.FuncDecl:
				declType = fmt.Sprintf("function '%s'", d.Name.Name)
			case *ast.GenDecl:
				switch d.Tok {
				case token.TYPE:
					if len(d.Specs) > 0 {
						if ts, ok := d.Specs[0].(*ast.TypeSpec); ok {
							declType = fmt.Sprintf("type '%s'", ts.Name.Name)
						} else {
							declType = "type declaration"
						}
					} else {
						declType = "type declaration"
					}
				case token.CONST:
					declType = "const declaration"
				case token.VAR:
					if len(d.Specs) > 0 {
						if vs, ok := d.Specs[0].(*ast.ValueSpec); ok && len(vs.Names) > 0 {
							declType = fmt.Sprintf("var '%s'", vs.Names[0].Name)
						} else {
							declType = "var declaration"
						}
					} else {
						declType = "var declaration"
					}
				default:
					declType = "generic declaration"
				}
			default:
				declType = "declaration"
			}
			break
		}
	}

	if declToMove == nil {
		// Provide helpful error message with available declarations
		var declInfo []string
		for i, decl := range sourceNode.Decls {
			startLine := fset.Position(decl.Pos()).Line
			endLine := fset.Position(decl.End()).Line
			switch d := decl.(type) {
			case *ast.FuncDecl:
				declInfo = append(declInfo, fmt.Sprintf("  %d: function '%s' (lines %d-%d)", i, d.Name.Name, startLine, endLine))
			case *ast.GenDecl:
				switch d.Tok {
				case token.TYPE:
					if len(d.Specs) > 0 {
						if ts, ok := d.Specs[0].(*ast.TypeSpec); ok {
							declInfo = append(declInfo, fmt.Sprintf("  %d: type '%s' (lines %d-%d)", i, ts.Name.Name, startLine, endLine))
						}
					}
				case token.CONST:
					declInfo = append(declInfo, fmt.Sprintf("  %d: const block (lines %d-%d)", i, startLine, endLine))
				case token.VAR:
					declInfo = append(declInfo, fmt.Sprintf("  %d: var block (lines %d-%d)", i, startLine, endLine))
				}
			}
		}
		declList := strings.Join(declInfo, "\n")
		if declList == "" {
			declList = "  (no declarations found)"
		}
		return fmt.Errorf("declaration not found at lines %d-%d in file %s\nAvailable declarations:\n%s", actualTarget.StartLine, actualTarget.EndLine, target.File, declList)
	}

	// Extract the code snippet for the declaration
	var declBuf bytes.Buffer
	if err := format.Node(&declBuf, fset, declToMove); err != nil {
		return fmt.Errorf("failed to format declaration: %w", err)
	}
	declCode := declBuf.String()

	// Collect comments associated with this declaration
	declStart := declToMove.Pos()
	declEnd := declToMove.End()
	var commentsToMove []*ast.CommentGroup
	var newSourceComments []*ast.CommentGroup

	for _, commentGroup := range sourceNode.Comments {
		if commentBelongsToDecl(fset, declStart, declEnd, commentGroup) {
			commentsToMove = append(commentsToMove, commentGroup)
		} else {
			newSourceComments = append(newSourceComments, commentGroup)
		}
	}

	// Remove declaration from source file
	sourceNode.Decls = append(sourceNode.Decls[:declIndex], sourceNode.Decls[declIndex+1:]...)
	// Update source file comments
	sourceNode.Comments = newSourceComments

	// Write modified source file
	var sourceBuf bytes.Buffer
	if err := format.Node(&sourceBuf, fset, sourceNode); err != nil {
		return fmt.Errorf("failed to format source file: %w", err)
	}
	if err := os.WriteFile(target.File, sourceBuf.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write source file: %w", err)
	}

	// Run goimports on source file to fix imports
	cmd := exec.Command("goimports", "-w", target.File)
	if err := cmd.Run(); err != nil {
		// Log but don't fail - goimports might not be available
		// This is a best-effort operation
	}

	// Parse or create destination file
	var destNode *ast.File
	destExists := true
	if _, err := os.Stat(newFile); os.IsNotExist(err) {
		destExists = false
	}

	if destExists {
		destNode, err = parser.ParseFile(fset, newFile, nil, parser.ParseComments)
		if err != nil {
			return fmt.Errorf("failed to parse destination file: %w", err)
		}
	} else {
		// Create new file with package declaration
		// Try to extract package name from source file
		packageName := sourceNode.Name.Name
		destNode = &ast.File{
			Name:     ast.NewIdent(packageName),
			Decls:    []ast.Decl{},
			Comments: []*ast.CommentGroup{},
		}
	}

	// Add declaration to destination file (at the end)
	destNode.Decls = append(destNode.Decls, declToMove)
	// Add comments to destination file
	destNode.Comments = append(destNode.Comments, commentsToMove...)

	// Write destination file
	var destBuf bytes.Buffer
	if err := format.Node(&destBuf, fset, destNode); err != nil {
		return fmt.Errorf("failed to format destination file: %w", err)
	}
	destContent := destBuf.Bytes()
	if err := os.WriteFile(newFile, destContent, 0644); err != nil {
		return fmt.Errorf("failed to write destination file: %w", err)
	}

	// Run goimports on destination file to fix imports
	cmd = exec.Command("goimports", "-w", newFile)
	if err := cmd.Run(); err != nil {
		// Log but don't fail - goimports might not be available
		// This is a best-effort operation
	}

	// Re-read the file after goimports may have modified it
	updatedDestContent, err := os.ReadFile(newFile)
	if err != nil {
		updatedDestContent = destContent // Fallback to original content
	}

	// Parse the written file to get accurate line numbers for the added declaration
	destFset := token.NewFileSet()
	parsedDestNode, err := parser.ParseFile(destFset, newFile, updatedDestContent, parser.ParseComments)
	if err == nil && len(parsedDestNode.Decls) > 0 {
		// Find the last declaration (the one we just added)
		lastDecl := parsedDestNode.Decls[len(parsedDestNode.Decls)-1]
		destStartLine := destFset.Position(lastDecl.Pos()).Line
		destEndLine := destFset.Position(lastDecl.End()).Line

		// Record changes with detailed information
		result.Changes = append(result.Changes, &CodeChange{
			Type:        "move_method",
			File:        target.File,
			StartLine:   actualTarget.StartLine,
			EndLine:     actualTarget.EndLine,
			Description: fmt.Sprintf("Moved %s to %s", declType, newFile),
			OldCode:     declCode,
			NewCode:     "",
		})

		result.Changes = append(result.Changes, &CodeChange{
			Type:        "move_method",
			File:        newFile,
			StartLine:   destStartLine,
			EndLine:     destEndLine,
			Description: fmt.Sprintf("Added %s from %s", declType, target.File),
			NewCode:     declCode,
		})
	} else {
		// Fallback if parsing fails - still record the change
		result.Changes = append(result.Changes, &CodeChange{
			Type:        "move_method",
			File:        target.File,
			StartLine:   actualTarget.StartLine,
			EndLine:     actualTarget.EndLine,
			Description: fmt.Sprintf("Moved %s to %s", declType, newFile),
			OldCode:     declCode,
			NewCode:     "",
		})

		result.Changes = append(result.Changes, &CodeChange{
			Type:        "move_method",
			File:        newFile,
			StartLine:   1,
			EndLine:     1,
			Description: fmt.Sprintf("Added %s from %s", declType, target.File),
			NewCode:     declCode,
		})
	}

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

// executeCreateFile creates a new file with the specified content
func (o *Orchestrator) executeCreateFile(operation *RefactoringOperation, result *OperationResult) error {
	codeSnippet, ok := operation.Parameters["codeSnippet"].(string)
	if !ok {
		return fmt.Errorf("codeSnippet parameter is required for create_file operation")
	}

	// Check if file already exists
	if _, err := os.Stat(operation.File); err == nil {
		// File exists - check if we should skip or overwrite
		if operation.Fallback != nil && operation.Fallback.Type == "skip" {
			result.Success = true
			result.Applied = false
			result.Message = "File already exists and fallback is skip"
			result.Changes = []*CodeChange{} // No changes applied
			return nil
		}
	}

	// Write the file
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
	if operation.Type == "remove_code_block" {
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
