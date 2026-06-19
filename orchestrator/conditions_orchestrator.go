package orchestrator

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// checkConditions verifies that all conditions attached to an operation are
// met. It returns (false, err) when a condition cannot be evaluated — an
// unevaluable condition is an error, never a silent pass.
func (o *Orchestrator) checkConditions(operation *RefactoringOperation) (bool, error) {
	if len(operation.Conditions) == 0 {
		return true, nil
	}
	for i, condition := range operation.Conditions {
		met, err := o.evaluateCondition(condition, operation)
		if err != nil {
			return false, fmt.Errorf("condition %d (%s): %w", i+1, condition.Type, err)
		}
		if !met {
			return false, nil
		}
	}
	return true, nil
}

// evaluateCondition evaluates a single condition against the current code
// state. Supported condition types:
//
//   - "complexity":      Property is a block metric (controlStructures,
//     statementCount, errorHandlingPaths, returnCount,
//     logicalOperators, maxNestingDepth) measured over the
//     operation's resolved target; Value is the threshold.
//   - "function_exists": Property is a function name (or "Receiver:Method");
//     Value is the expected boolean.
//   - "file_exists":     Property is a file path; Value is the expected boolean.
func (o *Orchestrator) evaluateCondition(condition *Condition, operation *RefactoringOperation) (bool, error) {
	if condition == nil {
		return false, fmt.Errorf("nil condition")
	}
	switch condition.Type {
	case "complexity":
		return o.evaluateComplexityCondition(condition, operation)
	case "function_exists":
		return o.evaluateFunctionExistsCondition(condition, operation)
	case "file_exists":
		return evaluateFileExistsCondition(condition)
	case "":
		return false, fmt.Errorf("condition type is required")
	default:
		return false, fmt.Errorf("unknown condition type %q (supported: complexity, function_exists, file_exists)", condition.Type)
	}
}

// evaluateComplexityCondition measures a block metric over the operation's
// resolved target and compares it against the condition value.
func (o *Orchestrator) evaluateComplexityCondition(condition *Condition, operation *RefactoringOperation) (bool, error) {
	if operation.File == "" {
		return false, fmt.Errorf("complexity condition requires an operation file")
	}
	if operation.Target == nil {
		return false, fmt.Errorf("complexity condition requires an operation target")
	}
	target, err := o.findTarget(operation.Target, operation.File)
	if err != nil {
		return false, fmt.Errorf("cannot resolve target for complexity condition: %w", err)
	}
	metrics, err := measureBlockMetrics(operation.File, target.StartLine, target.EndLine)
	if err != nil {
		return false, err
	}
	actual, err := metrics.property(condition.Property)
	if err != nil {
		return false, err
	}
	expected, ok := toFloat(condition.Value)
	if !ok {
		return false, fmt.Errorf("complexity condition value must be numeric, got %T (%v)", condition.Value, condition.Value)
	}
	return compareNumeric(float64(actual), defaultOperator(condition.Operator, "gte"), expected)
}

// evaluateFunctionExistsCondition checks whether a function or method
// ("Receiver:Method") is declared in the operation's file.
func (o *Orchestrator) evaluateFunctionExistsCondition(condition *Condition, operation *RefactoringOperation) (bool, error) {
	if condition.Property == "" {
		return false, fmt.Errorf("function_exists condition requires a function name in 'property'")
	}
	if operation.File == "" {
		return false, fmt.Errorf("function_exists condition requires an operation file")
	}
	name := condition.Property
	receiver := ""
	if idx := strings.Index(name, ":"); idx >= 0 {
		receiver = strings.TrimPrefix(name[:idx], "*")
		name = name[idx+1:]
	}
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, operation.File, nil, 0)
	if err != nil {
		return false, fmt.Errorf("cannot parse %s: %w", operation.File, err)
	}
	exists := false
	for _, decl := range node.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != name {
			continue
		}
		if receiver != "" && receiverTypeName(fn) != receiver {
			continue
		}
		exists = true
		break
	}
	return compareBool(exists, defaultOperator(condition.Operator, "eq"), condition.Value)
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
