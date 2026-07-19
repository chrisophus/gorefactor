package analyzer

import (
	"fmt"
	"go/ast"
	"sort"
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

// Summary returns a string summary of detected patterns
func (ap *ArchitecturalPattern) Summary() string {
	return fmt.Sprintf("[%s] %s: %s", strings.ToUpper(ap.Type), ap.Name, ap.Description)
}

// detectDataClumps finds typed parameter groups (>=3 params) that recur
// across >=2 functions in the file -- a Fowler "data clump" that likely
// wants to become its own struct. The signature is keyed on (name,type)
// pairs so int/string params are never conflated, order-normalized so a
// reordered group still matches, and emitted in deterministic order so
// the output does not depend on map iteration.
func (pd *PatternDetector) detectDataClumps() []ArchitecturalPattern {
	type clump struct {
		display string   // human-readable "name type, name type"
		params  []string // affected param names (first occurrence order)
		count   int
	}
	clumps := make(map[string]*clump)

	for _, decl := range pd.file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Type.Params == nil {
			continue
		}
		var pairs, names []string
		for _, field := range fn.Type.Params.List {
			t := paramTypeString(field.Type)
			for _, name := range field.Names {
				pairs = append(pairs, name.Name+" "+t)
				names = append(names, name.Name)
			}
		}
		if len(pairs) < 3 {
			continue
		}
		key := append([]string(nil), pairs...)
		sort.Strings(key)
		sig := strings.Join(key, ", ")
		c := clumps[sig]
		if c == nil {
			c = &clump{display: strings.Join(pairs, ", "), params: names}
			clumps[sig] = c
		}
		c.count++
	}

	var sigs []string
	for sig, c := range clumps {
		if c.count >= 2 {
			sigs = append(sigs, sig)
		}
	}
	sort.Strings(sigs)

	var patterns []ArchitecturalPattern
	for _, sig := range sigs {
		c := clumps[sig]
		patterns = append(patterns, ArchitecturalPattern{
			Name:        "Data Clumps",
			Type:        "smell",
			Severity:    "low",
			Description: fmt.Sprintf("Parameter group [%s] appears in %d functions; consider creating a struct", c.display, c.count),
			Affected:    c.params,
			Suggestion:  "Create a struct grouping these related fields, use it in function signatures",
		})
	}
	return patterns
}
