package orchestrator

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
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

// evaluateFileExistsCondition checks whether the file named by the condition
// property exists on disk.
func evaluateFileExistsCondition(condition *Condition) (bool, error) {
	if condition.Property == "" {
		return false, fmt.Errorf("file_exists condition requires a file path in 'property'")
	}
	_, err := os.Stat(condition.Property)
	exists := err == nil
	if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("cannot stat %s: %w", condition.Property, err)
	}
	return compareBool(exists, defaultOperator(condition.Operator, "eq"), condition.Value)
}

// blockMetrics are the complexity metrics measurable over a line range.
// They mirror the documented condition properties in ORCHESTRATION_SYSTEM.md.
type blockMetrics struct {
	controlStructures  int
	statementCount     int
	errorHandlingPaths int
	returnCount        int
	logicalOperators   int
	maxNestingDepth    int
}

func (m *blockMetrics) property(name string) (int, error) {
	switch name {
	case "controlStructures":
		return m.controlStructures, nil
	case "statementCount":
		return m.statementCount, nil
	case "errorHandlingPaths":
		return m.errorHandlingPaths, nil
	case "returnCount":
		return m.returnCount, nil
	case "logicalOperators":
		return m.logicalOperators, nil
	case "maxNestingDepth":
		return m.maxNestingDepth, nil
	}
	return 0, fmt.Errorf("unknown complexity property %q (supported: controlStructures, statementCount, errorHandlingPaths, returnCount, logicalOperators, maxNestingDepth)", name)
}

// measureBlockMetrics parses a file and computes block metrics for the nodes
// within the given line range.
func measureBlockMetrics(filePath string, startLine, endLine int) (*blockMetrics, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("cannot parse %s: %w", filePath, err)
	}
	m := &blockMetrics{}
	measureNode(node, fset, startLine, endLine, m, 0)
	return m, nil
}

// measureNode walks the children of n, counting metrics for nodes that fall
// inside [startLine, endLine]. Control structures recurse with depth+1 so
// maxNestingDepth reflects actual nesting.
func measureNode(n ast.Node, fset *token.FileSet, startLine, endLine int, m *blockMetrics, depth int) {
	ast.Inspect(n, func(child ast.Node) bool {
		if child == nil {
			return false
		}
		if child == n {
			return true
		}
		s := fset.Position(child.Pos()).Line
		e := fset.Position(child.End()).Line
		if e < startLine || s > endLine {
			return false // no overlap with the range; children cannot overlap either
		}
		inRange := s >= startLine && e <= endLine
		switch c := child.(type) {
		case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt:
			if inRange {
				m.controlStructures++
				m.statementCount++
				if ifStmt, ok := c.(*ast.IfStmt); ok && isErrNilCheck(ifStmt.Cond) {
					m.errorHandlingPaths++
				}
				if depth+1 > m.maxNestingDepth {
					m.maxNestingDepth = depth + 1
				}
			}
			measureNode(c, fset, startLine, endLine, m, depth+1)
			return false
		case *ast.ReturnStmt:
			if inRange {
				m.returnCount++
				m.statementCount++
			}
		case *ast.BinaryExpr:
			if inRange && (c.Op == token.LAND || c.Op == token.LOR) {
				m.logicalOperators++
			}
		case *ast.BlockStmt, *ast.CaseClause, *ast.CommClause:
			// containers, not statements
		case ast.Stmt:
			if inRange {
				m.statementCount++
			}
		}
		return true
	})
}

// isErrNilCheck reports whether a condition looks like an error-handling
// branch (an identifier named like an error compared against nil).
func isErrNilCheck(cond ast.Expr) bool {
	bin, ok := cond.(*ast.BinaryExpr)
	if !ok || (bin.Op != token.NEQ && bin.Op != token.EQL) {
		return false
	}
	isNil := func(e ast.Expr) bool {
		id, ok := e.(*ast.Ident)
		return ok && id.Name == "nil"
	}
	isErrIdent := func(e ast.Expr) bool {
		id, ok := e.(*ast.Ident)
		return ok && (id.Name == "err" || strings.HasSuffix(id.Name, "Err") || strings.HasSuffix(id.Name, "Error"))
	}
	return (isNil(bin.X) && isErrIdent(bin.Y)) || (isNil(bin.Y) && isErrIdent(bin.X))
}

func defaultOperator(op, def string) string {
	if op == "" {
		return def
	}
	return op
}

// compareNumeric applies a comparison operator to numeric values.
func compareNumeric(actual float64, operator string, expected float64) (bool, error) {
	switch operator {
	case "eq":
		return actual == expected, nil
	case "ne":
		return actual != expected, nil
	case "gt":
		return actual > expected, nil
	case "gte":
		return actual >= expected, nil
	case "lt":
		return actual < expected, nil
	case "lte":
		return actual <= expected, nil
	case "contains", "regex":
		return false, fmt.Errorf("operator %q is not applicable to numeric values", operator)
	}
	return false, fmt.Errorf("unknown operator %q (supported: eq, ne, gt, gte, lt, lte)", operator)
}

// compareBool compares an actual boolean state against the condition value.
// Beyond eq/ne it also supports contains/regex against the string form, so
// every documented operator has defined behavior.
func compareBool(actual bool, operator string, expected interface{}) (bool, error) {
	switch operator {
	case "eq", "ne":
		want, ok := toBool(expected)
		if !ok {
			return false, fmt.Errorf("condition value must be a boolean, got %T (%v)", expected, expected)
		}
		if operator == "eq" {
			return actual == want, nil
		}
		return actual != want, nil
	case "contains":
		s, ok := expected.(string)
		if !ok {
			return false, fmt.Errorf("contains operator requires a string value, got %T", expected)
		}
		return strings.Contains(fmt.Sprintf("%t", actual), s), nil
	case "regex":
		s, ok := expected.(string)
		if !ok {
			return false, fmt.Errorf("regex operator requires a string value, got %T", expected)
		}
		re, err := regexp.Compile(s)
		if err != nil {
			return false, fmt.Errorf("invalid regex %q: %w", s, err)
		}
		return re.MatchString(fmt.Sprintf("%t", actual)), nil
	}
	return false, fmt.Errorf("unknown operator %q (supported: eq, ne, contains, regex)", operator)
}

// toFloat converts JSON-decoded numeric values to float64. encoding/json
// produces float64; int variants cover plans built in Go code.
func toFloat(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

func toBool(v interface{}) (bool, bool) {
	switch b := v.(type) {
	case bool:
		return b, true
	case string:
		if b == "true" {
			return true, true
		}
		if b == "false" {
			return false, true
		}
	}
	return false, false
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
