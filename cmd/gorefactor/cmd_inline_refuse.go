package main

import (
	"go/ast"
	"go/token"
)

// refuseComplexBody rejects bodies containing constructs that cannot be
// inlined safely: defer, go, closures, and recursion or self-reference.
func refuseComplexBody(fd *ast.FuncDecl, name string) error {
	var refusal error
	set := func(format string, a ...interface{}) {
		if refusal == nil {
			refusal = parseErrorf(format, a...)
		}
	}
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.DeferStmt:
			set("cannot inline %s: body contains defer", name)
		case *ast.GoStmt:
			set("cannot inline %s: body contains a go statement", name)
		case *ast.FuncLit:
			set("cannot inline %s: body contains a closure", name)
		case *ast.Ident:
			if v.Name == name {
				set("cannot inline %s: function is recursive or refers to itself", name)
			}
		}
		return refusal == nil
	})
	return refusal
}

// refuseStmtModeHazards rejects statement-mode bodies that declare variables,
// assign, or branch — splicing those into a caller scope risks capture.
func refuseStmtModeHazards(fd *ast.FuncDecl, name string) error {
	var refusal error
	set := func(format string, a ...interface{}) {
		if refusal == nil {
			refusal = parseErrorf(format, a...)
		}
	}
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.AssignStmt:
			if v.Tok == token.DEFINE {
				set("cannot inline %s: body declares variables (capture risk in caller scope)", name)
			} else {
				set("cannot inline %s: body assigns to variables", name)
			}
		case *ast.DeclStmt:
			set("cannot inline %s: body declares variables (capture risk in caller scope)", name)
		case *ast.LabeledStmt, *ast.BranchStmt:
			set("cannot inline %s: body contains labels or branch statements", name)
		case *ast.RangeStmt:
			set("cannot inline %s: body contains a range statement (declares variables)", name)
		case *ast.IncDecStmt:
			set("cannot inline %s: body mutates a variable (++/--)", name)
		}
		return refusal == nil
	})
	return refusal
}
