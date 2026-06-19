package main

import (
	"fmt"
	"go/ast"
	"go/token"
)

// processWrapErrorsInFunc finds and rewrites eligible bare-error-return
// patterns within fn. It only transforms unambiguous cases:
//
//  1. The if-stmt immediately follows an assignment whose RHS is a call expr
//     (so we know what function was called).
//  2. The if-body contains exactly one return with "err" as the last result.
//  3. The return is not already wrapped (not a call to fmt.Errorf/errors.Wrap etc.)
func processWrapErrorsInFunc(fset *token.FileSet, fn *ast.FuncDecl, file string, result *wrapErrorResult) {
	stmts := fn.Body.List
	for i, stmt := range stmts {
		ifStmt, ok := stmt.(*ast.IfStmt)
		if !ok {
			continue
		}
		// Must be `if err != nil` with no init statement.
		if !isErrNotNil(ifStmt) {
			continue
		}

		// Find the single bare `return err` in the body.
		retStmt, ok := singleBareErrReturn(ifStmt.Body)
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

		// Derive context: from init statement or from preceding assignment.
		context := wrapContextFromIfInit(fset, ifStmt)
		if context == "" && i > 0 {
			context = wrapContextFromPrecedingStmt(fset, stmts[i-1])
		}
		if context == "" {
			// Fall back to enclosing function name.
			context = camelToContext(fn.Name.Name)
		}

		line := fset.Position(retStmt.Pos()).Line

		// Build the new return expression.
		// Find the position of "err" in the return results to replace just it.
		newErrExpr := buildErrfExpr(context)

		// Replace the err ident in retStmt.Results with the new call.
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
