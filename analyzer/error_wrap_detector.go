package analyzer

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
)

// ErrorWrapIssue describes an un-wrapped error return at a package
// boundary (exported function).
type ErrorWrapIssue struct {
	Function string
	Line     int
	Message  string
}

// FileErrorWrapIssues flags exported functions in file that return the
// error type and contain `return err` (bare). The fix is usually
// `return fmt.Errorf("doing X: %w", err)` — context-dependent, so no
// autofix.
func FileErrorWrapIssues(file string) ([]ErrorWrapIssue, error) {
	fset := token.NewFileSet()
	astFile, err := parser.ParseFile(fset, file, nil, 0)
	if err != nil {
		return nil, err
	}
	var out []ErrorWrapIssue
	for _, decl := range astFile.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil || !fn.Name.IsExported() || !returnsError(fn) {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			ret, ok := n.(*ast.ReturnStmt)
			if !ok {
				return true
			}
			for _, expr := range ret.Results {
				ident, ok := expr.(*ast.Ident)
				if !ok || ident.Name != "err" {
					continue
				}
				line := fset.Position(ret.Pos()).Line
				out = append(out, ErrorWrapIssue{
					Function: fn.Name.Name,
					Line:     line,
					Message: fmt.Sprintf(
						"%s returns bare err at line %d — wrap with fmt.Errorf(\"<context>: %%w\", err) to preserve trace",
						fn.Name.Name, line,
					),
				})
			}
			return true
		})
	}
	return out, nil
}

func returnsError(fn *ast.FuncDecl) bool {
	if fn.Type.Results == nil {
		return false
	}
	for _, field := range fn.Type.Results.List {
		if ident, ok := field.Type.(*ast.Ident); ok && ident.Name == "error" {
			return true
		}
	}
	return false
}
