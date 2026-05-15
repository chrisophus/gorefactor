package analyzer

import (
	"go/ast"
	"go/token"
)

// useWalker walks the AST looking for symbol uses
type useWalker struct {
	ua    *UseAnalyzer
	query SymbolQuery
}

// inspectNode is called for each node during ast.Inspect
func (uw *useWalker) inspectNode(node ast.Node) bool {
	if node == nil {
		return true
	}

	switch n := node.(type) {
	case *ast.CallExpr:
		uw.visitCall(n)
	case *ast.AssignStmt:
		uw.visitAssign(n)
	case *ast.Field:
		uw.visitField(n)
	case *ast.ReturnStmt:
		uw.visitReturn(n)
	case *ast.DeferStmt:
		uw.visitDefer(n)
	case *ast.TypeAssertExpr:
		uw.visitTypeAssert(n)
	}

	return true
}

// visitCall handles function/method calls
func (uw *useWalker) visitCall(call *ast.CallExpr) {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		if fn.Name == uw.query.Name && uw.query.Receiver == "" {
			uw.ua.recordUse(SymbolUse{
				File:       uw.ua.currentFile,
				Line:       uw.ua.fset.Position(fn.Pos()).Line,
				Column:     uw.ua.fset.Position(fn.Pos()).Column,
				Context:    UsageCall,
				Snippet:    uw.ua.getCodeSnippet(uw.ua.fset.Position(fn.Pos()).Line),
				Type:       TypeFunction,
				SymbolName: fn.Name,
			})
		}
	case *ast.SelectorExpr:
		// Method call: receiver.Method()
		if fn.Sel.Name == uw.query.Name {
			receiverType := uw.ua.typeExprToString(fn.X)

			// Match if: no receiver specified in query, or receiver matches (with or without *)
			shouldMatch := false
			if uw.query.Receiver == "" {
				// Any receiver type is OK for method-name-only queries
				shouldMatch = true
			} else if uw.query.Receiver == receiverType {
				// Exact match
				shouldMatch = true
			} else if uw.query.Receiver == "*"+receiverType || "*"+uw.query.Receiver == receiverType {
				// Match with/without pointer
				shouldMatch = true
			}

			if shouldMatch {
				uw.ua.recordUse(SymbolUse{
					File:       uw.ua.currentFile,
					Line:       uw.ua.fset.Position(fn.Sel.Pos()).Line,
					Column:     uw.ua.fset.Position(fn.Sel.Pos()).Column,
					Context:    UsageCall,
					Snippet:    uw.ua.getCodeSnippet(uw.ua.fset.Position(fn.Sel.Pos()).Line),
					Type:       TypeMethod,
					SymbolName: fn.Sel.Name,
					Receiver:   receiverType,
				})
			}
		}
	}
}

// visitAssign handles assignments
func (uw *useWalker) visitAssign(assign *ast.AssignStmt) {
	// Check if RHS uses our symbol
	for _, expr := range assign.Rhs {
		if ident, ok := expr.(*ast.Ident); ok && ident.Name == uw.query.Name {
			uw.ua.recordUse(SymbolUse{
				File:       uw.ua.currentFile,
				Line:       uw.ua.fset.Position(ident.Pos()).Line,
				Column:     uw.ua.fset.Position(ident.Pos()).Column,
				Context:    UsageRead,
				Snippet:    uw.ua.getCodeSnippet(uw.ua.fset.Position(ident.Pos()).Line),
				SymbolName: ident.Name,
			})
		}
	}

	// Check if LHS assigns to our symbol (with :=)
	if assign.Tok == token.DEFINE || assign.Tok == token.ASSIGN {
		for _, expr := range assign.Lhs {
			if ident, ok := expr.(*ast.Ident); ok && ident.Name == uw.query.Name {
				ctx := UsageWrite
				if assign.Tok == token.DEFINE {
					ctx = UsageDefine
				}
				uw.ua.recordUse(SymbolUse{
					File:       uw.ua.currentFile,
					Line:       uw.ua.fset.Position(ident.Pos()).Line,
					Column:     uw.ua.fset.Position(ident.Pos()).Column,
					Context:    ctx,
					Snippet:    uw.ua.getCodeSnippet(uw.ua.fset.Position(ident.Pos()).Line),
					SymbolName: ident.Name,
				})
			}
		}
	}
}

// visitField handles struct fields
func (uw *useWalker) visitField(field *ast.Field) {
	// This is for finding field uses, more complex
}

// visitReturn handles return statements
func (uw *useWalker) visitReturn(ret *ast.ReturnStmt) {
	for _, expr := range ret.Results {
		if ident, ok := expr.(*ast.Ident); ok && ident.Name == uw.query.Name {
			uw.ua.recordUse(SymbolUse{
				File:       uw.ua.currentFile,
				Line:       uw.ua.fset.Position(ident.Pos()).Line,
				Column:     uw.ua.fset.Position(ident.Pos()).Column,
				Context:    UsageReturn,
				Snippet:    uw.ua.getCodeSnippet(uw.ua.fset.Position(ident.Pos()).Line),
				SymbolName: ident.Name,
			})
		}
	}
}

// visitDefer handles defer statements
func (uw *useWalker) visitDefer(defer_ *ast.DeferStmt) {
	if call, ok := defer_.Call.Fun.(*ast.Ident); ok && call.Name == uw.query.Name {
		uw.ua.recordUse(SymbolUse{
			File:       uw.ua.currentFile,
			Line:       uw.ua.fset.Position(call.Pos()).Line,
			Column:     uw.ua.fset.Position(call.Pos()).Column,
			Context:    UsageDefer,
			Snippet:    uw.ua.getCodeSnippet(uw.ua.fset.Position(call.Pos()).Line),
			SymbolName: call.Name,
		})
	}
}

// visitTypeAssert handles type assertions
func (uw *useWalker) visitTypeAssert(typeAssert *ast.TypeAssertExpr) {
	if ident, ok := typeAssert.Type.(*ast.Ident); ok && ident.Name == uw.query.Name {
		uw.ua.recordUse(SymbolUse{
			File:       uw.ua.currentFile,
			Line:       uw.ua.fset.Position(ident.Pos()).Line,
			Column:     uw.ua.fset.Position(ident.Pos()).Column,
			Context:    UsageType,
			Snippet:    uw.ua.getCodeSnippet(uw.ua.fset.Position(ident.Pos()).Line),
			SymbolName: ident.Name,
		})
	}
}
