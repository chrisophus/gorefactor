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
