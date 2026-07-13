package main

import (
	"fmt"
	"go/ast"
	"go/token"
)

// processWrapErrorsInFunc finds and rewrites eligible bare-error-return
// patterns within fn. It delegates to processStmtList which recurses into
// nested compound statements (for, range, switch, etc.).
func processWrapErrorsInFunc(fset *token.FileSet, fn *ast.FuncDecl, file string, result *wrapErrorResult) {
	processStmtList(fset, fn, fn.Body.List, file, result)
}

// processStmtList walks a list of statements, looking for `if err != nil`
// blocks to transform and recursing into nested block-producing statements
// (for, range, select, switch, etc.).
//
// stmts is the list to walk; fn is the enclosing function (used for context
// derivation and skip reporting). The stmts slice is walked with index-
// awareness so that the statement preceding an if-block can be used to
// derive wrapping context (e.g. from an assignment RHS).
func processStmtList(fset *token.FileSet, fn *ast.FuncDecl, stmts []ast.Stmt, file string, result *wrapErrorResult) {
	extractBlockL25(stmts, fset, fn, file, result)
}

func extractBlockL25(stmts []ast.Stmt, fset *token.FileSet, fn *ast.FuncDecl, file string, result *wrapErrorResult) {
	for i, stmt := range stmts {
		ifStmt, ok := stmt.(*ast.IfStmt)
		if !ok {

			recurseIntoStmt(fset, fn, stmt, file, result)
			continue
		}

		if !isErrNotNil(ifStmt) {
			recurseIntoStmt(fset, fn, ifStmt, file, result)
			continue
		}

		retStmt, ok := findBareErrReturn(ifStmt.Body)
		if !ok {
			line := fset.Position(ifStmt.Pos()).Line
			result.Skipped++
			result.Reasons = append(result.Reasons, wrapSkipReason{
				File:     file,
				Function: fn.Name.Name,
				Line:     line,
				Reason:   "if body has multiple returns or no bare err return",
			})
			continue
		}

		context := wrapContextFromIfInit(fset, ifStmt)
		if context == "" && i > 0 {
			context = wrapContextFromPrecedingStmt(fset, stmts[i-1])
		}
		if context == "" {

			context = camelToContext(fn.Name.Name)
		}

		line := fset.Position(retStmt.Pos()).Line

		errIdent, _ := retStmt.Results[len(retStmt.Results)-1].(*ast.Ident)
		var errPos token.Pos
		if errIdent != nil {
			errPos = errIdent.NamePos
		}
		newErrExpr := buildErrfExpr(context, errPos)

		replaced := replaceErrInReturn(retStmt, newErrExpr)
		if !replaced {
			result.Skipped++
			result.Reasons = append(result.Reasons, wrapSkipReason{
				File:     file,
				Function: fn.Name.Name,
				Line:     line,
				Reason:   "could not locate err in return results",
			})
			continue
		}

		result.Transformed++
		result.Changes = append(result.Changes, wrapChangeRecord{
			File:     file,
			Function: fn.Name.Name,
			Line:     line,
			OldText:  "return err",
			NewText:  fmt.Sprintf("return fmt.Errorf(\"%s: %%w\", err)", context),
		})
	}
}

// recurseIntoStmt recurses into the child statement lists of compound
// statements (for, range, select, switch, if-else chains, etc.).
func recurseIntoStmt(fset *token.FileSet, fn *ast.FuncDecl, stmt ast.Stmt, file string, result *wrapErrorResult) {
	switch s := stmt.(type) {
	case *ast.ForStmt:
		if s.Body != nil {
			processStmtList(fset, fn, s.Body.List, file, result)
		}
	case *ast.RangeStmt:
		if s.Body != nil {
			processStmtList(fset, fn, s.Body.List, file, result)
		}
	case *ast.BlockStmt:
		processStmtList(fset, fn, s.List, file, result)
	case *ast.IfStmt:
		if s.Body != nil {
			processStmtList(fset, fn, s.Body.List, file, result)
		}
		if s.Else != nil {
			recurseIntoStmt(fset, fn, s.Else, file, result)
		}
	case *ast.SelectStmt:
		if s.Body != nil {
			for _, c := range s.Body.List {
				if cc, ok := c.(*ast.CommClause); ok {
					processStmtList(fset, fn, cc.Body, file, result)
				}
			}
		}
	case *ast.SwitchStmt:
		if s.Body != nil {
			for _, c := range s.Body.List {
				if cc, ok := c.(*ast.CaseClause); ok {
					processStmtList(fset, fn, cc.Body, file, result)
				}
			}
		}
	case *ast.TypeSwitchStmt:
		if s.Body != nil {
			for _, c := range s.Body.List {
				if cc, ok := c.(*ast.CaseClause); ok {
					processStmtList(fset, fn, cc.Body, file, result)
				}
			}
		}
	}
}
