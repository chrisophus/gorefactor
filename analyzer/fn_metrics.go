package analyzer

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
)

// FunctionMetrics holds per-function structural metrics used by the review
// command and the long-function / deep-nesting / hard-to-maintain lint rules.
type FunctionMetrics struct {
	Name       string `json:"name"`
	Receiver   string `json:"receiver,omitempty"`
	Line       int    `json:"line"`
	EndLine    int    `json:"endLine"`
	Lines      int    `json:"lines"`
	Complexity int    `json:"complexity"`
	MaxNesting int    `json:"maxNesting"`
	// ErrorPaths counts if-bodies whose first statement is a return (early-exit
	// density). Paired with length in hard-to-maintain so long straight-line
	// orchestrators stay quiet while long+branchy error ladders do not.
	ErrorPaths int `json:"errorPaths"`
	// LiteralLines is the count of source lines occupied by literal data in
	// the body: composite literals (slice/map/struct catalogs) and multi-line
	// string literals (templates, prompts, embedded text). A declarative
	// catalog is long in data, not logic; LogicLines subtracts it so length
	// rules measure logic.
	LiteralLines int `json:"literalLines"`
	// Dispatch is the per-branch re-scoring for table-shaped functions
	// (nil when the body has no eligible top-level dispatch switch).
	Dispatch *DispatchInfo `json:"dispatch,omitempty"`
}

// LogicLines returns the function's length excluding composite-literal data
// lines, so a 180-line catalog of `[]T{...}` reads as a few logic lines.
func (m FunctionMetrics) LogicLines() int {
	n := m.Lines - m.LiteralLines
	if n < 0 {
		return 0
	}
	return n
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
	// parser.ParseFile takes src as `any`: a typed-nil []byte is a non-nil
	// interface, which readSource treats as empty source instead of "read the
	// file from disk". Convert to an untyped nil so the from-disk contract in
	// this function's doc comment actually holds (this had silently disabled
	// every FunctionMetricsForFile-based lint rule).
	var srcAny any
	if src != nil {
		srcAny = src
	}
	astFile, err := parser.ParseFile(fset, filename, srcAny, 0)
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
			Name:         fn.Name.Name,
			Receiver:     receiverTypeName(fn),
			Line:         start,
			EndLine:      end,
			Lines:        end - start + 1,
			Complexity:   calculateFunctionComplexity(fn),
			MaxNesting:   functionMaxNesting(fn),
			ErrorPaths:   functionErrorPaths(fn),
			LiteralLines: compositeLiteralLines(fset, fn.Body),
			Dispatch:     AnalyzeDispatch(fset, fn),
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
	return strings.TrimPrefix(types.ExprString(t), "*")
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

func functionErrorPaths(fn *ast.FuncDecl) int {
	if fn.Body == nil {
		return 0
	}
	n := 0
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		ifs, ok := node.(*ast.IfStmt)
		if !ok || ifs.Body == nil || len(ifs.Body.List) == 0 {
			return true
		}
		if _, ok := ifs.Body.List[0].(*ast.ReturnStmt); ok {
			n++
		}
		return true
	})
	return n
}

// compositeLiteralLines counts the distinct source lines occupied by literal data within body:
// composite literals (slice/map/struct catalogs) and multi-line string literals (templates,
// prompts). Nested and sibling literals are de-duplicated by line, so a catalog like `return
// []Tool{ {...}, {...} }` reports the literal's full span once.
func compositeLiteralLines(fset *token.FileSet, body *ast.BlockStmt) int {
	if body == nil {
		return 0
	}
	lines := map[int]bool{}
	mark := func(from, to token.Pos) {
		start := fset.Position(from).Line
		end := fset.Position(to).Line
		for l := start; l <= end; l++ {
			lines[l] = true
		}
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch lit := n.(type) {
		case *ast.CompositeLit:
			mark(lit.Lbrace, lit.Rbrace)
		case *ast.BasicLit:
			// A multi-line string literal (raw-string template, prompt text,
			// embedded document) is data too; single-line strings stay logic.
			if lit.Kind == token.STRING && fset.Position(lit.End()).Line > fset.Position(lit.Pos()).Line {
				mark(lit.Pos(), lit.End())
			}
		}
		return true
	})
	return len(lines)

}
