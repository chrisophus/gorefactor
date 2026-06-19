package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"strings"

	"github.com/chrisophus/gorefactor/orchestrator"
)

var wrapErrorsFlags = mutFlagSpec(nil)

func init() {
	registerCommand(Command{
		Name:        "wrap-errors",
		Description: "Rewrite bare 'return err' in 'if err != nil' blocks to fmt.Errorf wrapping with context",
		Usage:       "wrap-errors <file> [<Func|Receiver:Method>] [--json] [--dry-run] [--gate]",
		MinArgs:     1,
		MaxArgs:     2,
		Flags:       wrapErrorsFlags,
		Run:         wrapErrorsCommand,
	})
}

// wrapErrorResult is the structured summary returned by wrap-errors.
type wrapErrorResult struct {
	Transformed int                `json:"transformed"`
	Skipped     int                `json:"skipped"`
	Reasons     []wrapSkipReason   `json:"skipped_reasons,omitempty"`
	Changes     []wrapChangeRecord `json:"changes,omitempty"`
}

type wrapSkipReason struct {
	File     string `json:"file"`
	Function string `json:"function"`
	Line     int    `json:"line"`
	Reason   string `json:"reason"`
}

type wrapChangeRecord struct {
	File     string `json:"file"`
	Function string `json:"function"`
	Line     int    `json:"line"`
	OldText  string `json:"old"`
	NewText  string `json:"new"`
}

func wrapErrorsCommand(args []string) error {
	pos, flags := parseFlags(args, wrapErrorsFlags)
	if len(pos) < 1 {
		return usageErrorf("usage: wrap-errors <file> [<Func|Receiver:Method>]")
	}
	file := pos[0]
	var funcFilter string
	if len(pos) >= 2 {
		funcFilter = pos[1]
	}
	m := &mutation{op: "wrap-errors", file: file}
	m.setCommonFlags(flags)

	return m.run(func() (string, error) {
		return applyWrapErrors(file, funcFilter)
	})
}

// applyWrapErrors rewrites the file in-place and returns a human summary.
func applyWrapErrors(file, funcFilter string) (string, error) {
	src, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	fset := token.NewFileSet()
	astFile, err := parser.ParseFile(fset, file, src, parser.ParseComments)
	if err != nil {
		return "", parseErrorf("parse %s: %v", file, err)
	}

	// Collect targeted function names.
	var wantFunc string
	if funcFilter != "" {
		// Accept both "Func" and "Receiver:Method" forms.
		if idx := strings.Index(funcFilter, ":"); idx >= 0 {
			wantFunc = funcFilter[idx+1:]
		} else {
			wantFunc = funcFilter
		}
	}

	var result wrapErrorResult

	for _, decl := range astFile.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		if wantFunc != "" && fn.Name.Name != wantFunc {
			continue
		}
		if !funcReturnsError(fn) {
			continue
		}
		processWrapErrorsInFunc(fset, fn, file, &result)
	}

	if result.Transformed == 0 {
		summary := fmt.Sprintf("wrap-errors: %d transformed, %d skipped — nothing changed", 0, result.Skipped)
		if result.Skipped > 0 && len(result.Reasons) > 0 {
			for _, r := range result.Reasons {
				summary += fmt.Sprintf("\n  skip %s:%d (%s): %s", r.File, r.Line, r.Function, r.Reason)
			}
		}
		return summary, nil
	}

	// Re-render the modified AST.
	var buf bytes.Buffer
	if err := format.Node(&buf, fset, astFile); err != nil {
		return "", parseErrorf("internal: re-format after wrap-errors failed: %v", err)
	}
	if err := os.WriteFile(file, buf.Bytes(), 0644); err != nil {
		return "", err
	}
	if err := orchestrator.FormatImports(file); err != nil {
		fmt.Fprintf(os.Stderr, "warning: format imports on %s: %v\n", file, err)
	}
	summary := fmt.Sprintf("wrap-errors: %d transformed, %d skipped", result.Transformed, result.Skipped)
	if result.Skipped > 0 && len(result.Reasons) > 0 {
		for _, r := range result.Reasons {
			summary += fmt.Sprintf("\n  skip %s:%d (%s): %s", r.File, r.Line, r.Function, r.Reason)
		}
	}
	return summary, nil
}

// funcReturnsError checks if a function declaration has an error return type.
func funcReturnsError(fn *ast.FuncDecl) bool {
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

// isErrNotNil reports whether the if-stmt's condition is `err != nil`.
// Handles both `if err != nil` and `if err := ...; err != nil`.
func isErrNotNil(ifStmt *ast.IfStmt) bool {
	be, ok := ifStmt.Cond.(*ast.BinaryExpr)
	if !ok || be.Op.String() != "!=" {
		return false
	}
	lhsIdent, lok := be.X.(*ast.Ident)
	rhsIdent, rok := be.Y.(*ast.Ident)
	return lok && rok && lhsIdent.Name == "err" && rhsIdent.Name == "nil"
}

// singleBareErrReturn returns the sole return statement in the if-body when
// the body has exactly one statement that is a return containing "err" as
// the last result value. Returns (stmt, true) on success.
func singleBareErrReturn(body *ast.BlockStmt) (*ast.ReturnStmt, bool) {
	if len(body.List) != 1 {
		return nil, false
	}
	ret, ok := body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) == 0 {
		return nil, false
	}
	last := ret.Results[len(ret.Results)-1]
	ident, ok := last.(*ast.Ident)
	if !ok || ident.Name != "err" {
		return nil, false
	}
	// Ensure it's not already wrapped (e.g. `return 0, fmt.Errorf(...)`)
	// — actually the ident check above already covers this: if it's a call
	// expr the ident check fails.
	return ret, true
}

// wrapContextFromIfInit derives a context string from an init statement
// like `if err := doThing(); err != nil`.
func wrapContextFromIfInit(_ *token.FileSet, ifStmt *ast.IfStmt) string {
	if ifStmt.Init == nil {
		return ""
	}
	assign, ok := ifStmt.Init.(*ast.AssignStmt)
	if !ok {
		return ""
	}
	if len(assign.Rhs) != 1 {
		return ""
	}
	return contextFromExpr(assign.Rhs[0])
}

// wrapContextFromPrecedingStmt derives context from the statement before the
// if-block (typically an assignment whose RHS is the call that produced err).
func wrapContextFromPrecedingStmt(_ *token.FileSet, stmt ast.Stmt) string {
	switch s := stmt.(type) {
	case *ast.AssignStmt:
		if len(s.Rhs) == 1 {
			return contextFromExpr(s.Rhs[0])
		}
	case *ast.ExprStmt:
		return contextFromExpr(s.X)
	}
	return ""
}

// contextFromExpr extracts a short context label from an expression (usually
// a call expression). Returns "" when the expression is not a call.
func contextFromExpr(expr ast.Expr) string {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return ""
	}
	return callContext(call.Fun)
}

// callContext renders a short name for the function being called.
func callContext(fun ast.Expr) string {
	switch f := fun.(type) {
	case *ast.Ident:
		return camelToContext(f.Name)
	case *ast.SelectorExpr:
		return camelToContext(f.Sel.Name)
	}
	return ""
}

// camelToContext converts CamelCase to "camel case" style context strings.
// "FetchUser" → "fetch user", "doThing" → "do thing".
func camelToContext(name string) string {
	if name == "" {
		return ""
	}
	var parts []string
	start := 0
	for i := 1; i < len(name); i++ {
		if isUpper(name[i]) && !isUpper(name[i-1]) {
			parts = append(parts, strings.ToLower(name[start:i]))
			start = i
		}
	}
	parts = append(parts, strings.ToLower(name[start:]))
	return strings.Join(parts, " ")
}

func isUpper(b byte) bool { return b >= 'A' && b <= 'Z' }

// buildErrfExpr returns an AST call expression for
// fmt.Errorf("<context>: %w", err).
func buildErrfExpr(context string) *ast.CallExpr {
	fmtPkg := &ast.Ident{Name: "fmt"}
	errorfFn := &ast.SelectorExpr{X: fmtPkg, Sel: &ast.Ident{Name: "Errorf"}}
	fmtStr := &ast.BasicLit{
		Kind:  token.STRING,
		Value: fmt.Sprintf(`"%s: %%w"`, context),
	}
	errIdent := &ast.Ident{Name: "err"}
	return &ast.CallExpr{
		Fun:  errorfFn,
		Args: []ast.Expr{fmtStr, errIdent},
	}
}

// replaceErrInReturn rewrites the last result of ret from the bare `err`
// identifier to newExpr. Returns true if the replacement was made.
func replaceErrInReturn(ret *ast.ReturnStmt, newExpr ast.Expr) bool {
	if len(ret.Results) == 0 {
		return false
	}
	last := len(ret.Results) - 1
	if ident, ok := ret.Results[last].(*ast.Ident); !ok || ident.Name != "err" {
		return false
	}
	ret.Results[last] = newExpr
	return true
}
