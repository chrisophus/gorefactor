package orchestrator

import (
	"go/ast"
	"go/token"
)

// nonReferenceIdents collects identifiers that are not plain references:
// declaration names, selector members, struct-literal keys, field names,
// labels, and import aliases. Renaming passes must leave these alone.
func nonReferenceIdents(root ast.Node) map[*ast.Ident]bool {
	skip := map[*ast.Ident]bool{}
	ast.Inspect(root, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.SelectorExpr:
			skip[x.Sel] = true
		case *ast.KeyValueExpr:
			// Likely a struct-literal field name. (Map-literal keys that are
			// identifiers are mistakenly skipped too; acceptable heuristic
			// without type information.)
			if id, ok := x.Key.(*ast.Ident); ok {
				skip[id] = true
			}
		case *ast.Field:
			for _, nm := range x.Names {
				skip[nm] = true
			}
		case *ast.FuncDecl:
			skip[x.Name] = true
		case *ast.TypeSpec:
			skip[x.Name] = true
		case *ast.ValueSpec:
			for _, nm := range x.Names {
				skip[nm] = true
			}
		case *ast.AssignStmt:
			if x.Tok == token.DEFINE {
				for _, l := range x.Lhs {
					if id, ok := l.(*ast.Ident); ok {
						skip[id] = true
					}
				}
			}
		case *ast.LabeledStmt:
			skip[x.Label] = true
		case *ast.BranchStmt:
			if x.Label != nil {
				skip[x.Label] = true
			}
		case *ast.ImportSpec:
			if x.Name != nil {
				skip[x.Name] = true
			}
		}
		return true
	})
	return skip
}
