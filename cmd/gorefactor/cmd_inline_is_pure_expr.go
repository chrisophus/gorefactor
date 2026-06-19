package main

import (
	"go/ast"
	"go/token"
)

// isPureExpr reports whether evaluating e is side-effect-free (no calls,
// channel ops, or closures). Pure expressions may be substituted textually.
func isPureExpr(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.Ident, *ast.BasicLit:
		return true
	case *ast.SelectorExpr:
		return isPureExpr(v.X)
	case *ast.ParenExpr:
		return isPureExpr(v.X)
	case *ast.StarExpr:
		return isPureExpr(v.X)
	case *ast.UnaryExpr:
		return v.Op != token.ARROW && isPureExpr(v.X)
	case *ast.BinaryExpr:
		return isPureExpr(v.X) && isPureExpr(v.Y)
	case *ast.IndexExpr:
		return isPureExpr(v.X) && isPureExpr(v.Index)
	case *ast.SliceExpr:
		for _, sub := range []ast.Expr{v.X, v.Low, v.High, v.Max} {
			if sub != nil && !isPureExpr(sub) {
				return false
			}
		}
		return true
	case *ast.CompositeLit:
		for _, elt := range v.Elts {
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				if !isPureExpr(kv.Value) {
					return false
				}
				continue
			}
			if !isPureExpr(elt) {
				return false
			}
		}
		return true
	}
	return false
}
