package orchestrator

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
)

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
