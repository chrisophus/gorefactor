package analyzer

import (
	"fmt"
	"go/ast"
	"strings"
)

// PatternDetector identifies architectural patterns and code smells
type PatternDetector struct {
	file *ast.File
}

// ArchitecturalPattern represents a detected pattern or smell
type ArchitecturalPattern struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`     // "smell" or "pattern"
	Severity    string   `json:"severity"` // low, medium, high
	Description string   `json:"description"`
	Affected    []string `json:"affected"` // function/type names
	Suggestion  string   `json:"suggestion"`
}

// NewPatternDetector creates a detector for a file
func NewPatternDetector(file *ast.File) *PatternDetector {
	return &PatternDetector{file: file}
}

// Helper functions

// getReceiverTypeName extracts the receiver type name from a field
func getReceiverTypeName(field *ast.Field) string {
	if star, ok := field.Type.(*ast.StarExpr); ok {
		if ident, ok := star.X.(*ast.Ident); ok {
			return ident.Name
		}
	}
	if ident, ok := field.Type.(*ast.Ident); ok {
		return ident.Name
	}
	return "Unknown"
}

func paramTypeString(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		return "*" + paramTypeString(e.X)
	case *ast.SelectorExpr:
		return paramTypeString(e.X) + "." + e.Sel.Name
	case *ast.ArrayType:
		return "[]" + paramTypeString(e.Elt)
	case *ast.MapType:
		return "map[" + paramTypeString(e.Key) + "]" + paramTypeString(e.Value)
	case *ast.Ellipsis:
		return "..." + paramTypeString(e.Elt)
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.FuncType:
		return "func"
	default:
		return "expr"
	}
}

// countSwitchStatements recursively counts switch statements in a block
func countSwitchStatements(block *ast.BlockStmt) int {
	if block == nil {
		return 0
	}

	count := 0
	for _, stmt := range block.List {
		count += countSwitchInStmt(stmt)
	}
	return count
}

// countSwitchInStmt recursively counts switch statements
func countSwitchInStmt(stmt ast.Stmt) int {
	count := 0
	switch s := stmt.(type) {
	case *ast.TypeSwitchStmt:
		count++
		if s.Body != nil {
			for _, s2 := range s.Body.List {
				count += countSwitchInStmt(s2)
			}
		}
	case *ast.IfStmt:
		if s.Body != nil {
			count += countSwitchStatements(s.Body)
		}
		if s.Else != nil {
			count += countSwitchInStmt(s.Else)
		}
	case *ast.ForStmt:
		if s.Body != nil {
			count += countSwitchStatements(s.Body)
		}
	case *ast.RangeStmt:
		if s.Body != nil {
			count += countSwitchStatements(s.Body)
		}
	case *ast.BlockStmt:
		count += countSwitchStatements(s)
	}
	return count
}

// priorityFromSeverity converts severity to priority score
func priorityFromSeverity(severity string) int {
	switch severity {
	case "high":
		return 9
	case "medium":
		return 6
	case "low":
		return 3
	default:
		return 5
	}
}

// Summary returns a string summary of detected patterns
func (ap *ArchitecturalPattern) Summary() string {
	return fmt.Sprintf("[%s] %s: %s", strings.ToUpper(ap.Type), ap.Name, ap.Description)
}
