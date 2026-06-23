package analyzer

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// FunctionMetrics holds per-function structural metrics used by the review
// command and the long-function / deep-nesting lint rules.
type FunctionMetrics struct {
	Name       string `json:"name"`
	Receiver   string `json:"receiver,omitempty"`
	Line       int    `json:"line"`
	EndLine    int    `json:"endLine"`
	Lines      int    `json:"lines"`
	Complexity int    `json:"complexity"`
	MaxNesting int    `json:"maxNesting"`
}

// Key returns "Receiver:Name" for methods or "Name" for plain functions,
// matching the CLI's Receiver:Method locator convention.
func (m FunctionMetrics) Key() string {
	if m.Receiver != "" {
		return m.Receiver + ":" + m.Name
	}
	return m.Name
}

// FunctionMetricsForFile parses a file from disk and returns metrics for
// every function and method declaration.
func FunctionMetricsForFile(file string) ([]FunctionMetrics, error) {
	return FunctionMetricsForSource(file, nil)
}

// FunctionMetricsForSource parses src (filename is used for positions only;
// when src is nil the file is read from disk) and returns metrics for every
// function and method declaration.
func FunctionMetricsForSource(filename string, src []byte) ([]FunctionMetrics, error) {
	fset := token.NewFileSet()
	astFile, err := parser.ParseFile(fset, filename, src, 0)
	if err != nil {
		return nil, fmt.Errorf("parse file: %w", err)
	}
	var out []FunctionMetrics
	for _, decl := range astFile.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		start := fset.Position(fn.Pos()).Line
		end := fset.Position(fn.End()).Line
		out = append(out, FunctionMetrics{
			Name:       fn.Name.Name,
			Receiver:   receiverTypeName(fn),
			Line:       start,
			EndLine:    end,
			Lines:      end - start + 1,
			Complexity: calculateFunctionComplexity(fn),
			MaxNesting: functionMaxNesting(fn),
		})
	}
	return out, nil
}

// receiverTypeName returns the bare receiver type name (no "*", no type
// parameters) of a method declaration, or "" for plain functions.
func receiverTypeName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return ""
	}
	t := fn.Recv.List[0].Type
	if star, ok := t.(*ast.StarExpr); ok {
		t = star.X
	}
	if idx, ok := t.(*ast.IndexExpr); ok { // generic receiver T[P]
		t = idx.X
	}
	if idx, ok := t.(*ast.IndexListExpr); ok { // generic receiver T[P1, P2]
		t = idx.X
	}
	if id, ok := t.(*ast.Ident); ok {
		return id.Name
	}
	return strings.TrimPrefix(exprString(t), "*")
}

func exprString(e ast.Expr) string {
	if id, ok := e.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

// functionMaxNesting computes the deepest nesting of control structures
// (if / for / range / switch / type-switch / select) inside a function body.
func functionMaxNesting(fn *ast.FuncDecl) int {
	if fn.Body == nil {
		return 0
	}
	max := 0
	var ends []token.Pos // End() positions of currently-open control nodes
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if n == nil {
			return true
		}
		for len(ends) > 0 && n.Pos() >= ends[len(ends)-1] {
			ends = ends[:len(ends)-1]
		}
		switch n.(type) {
		case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt:
			ends = append(ends, n.End())
			if len(ends) > max {
				max = len(ends)
			}
		}
		return true
	})
	return max
}
