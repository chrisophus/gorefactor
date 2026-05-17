package main

import (
	"fmt"
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/packages"
)

func analyzeBlockTypes(pkg *packages.Package, fileAST *ast.File, enclosing *ast.FuncDecl, stmts []ast.Stmt) (params, returns []paramSpec, err error) {
	info := pkg.TypesInfo
	if info == nil {
		return nil, nil, fmt.Errorf("types info missing (package may have compile errors)")
	}
	declaredInBlock := map[types.Object]bool{}
	for _, stmt := range stmts {
		ast.Inspect(stmt, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				if obj := info.Defs[id]; obj != nil {
					declaredInBlock[obj] = true
				}
			}
			return true
		})
	}

	blockStart := stmts[0].Pos()
	blockEnd := stmts[len(stmts)-1].End()

	seenRead := map[types.Object]bool{}
	for _, stmt := range stmts {
		ast.Inspect(stmt, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok {
				return true
			}
			obj := info.Uses[id]
			if obj == nil {
				return true
			}
			if declaredInBlock[obj] {
				return true
			}
			if _, isVar := obj.(*types.Var); !isVar {
				return true
			}
			if obj.Pkg() == nil || obj.Pkg().Path() != pkg.PkgPath {
				return true
			}
			if seenRead[obj] {
				return true
			}
			if !isLocalToFunc(obj, enclosing, info) {
				return true
			}
			seenRead[obj] = true
			params = append(params, paramSpec{
				name:   obj.Name(),
				typeS:  types.TypeString(obj.Type(), relativeToPkg(pkg.Types)),
				object: obj,
			})
			return true
		})
	}

	usedAfterBlock := func(obj types.Object) bool {
		for id, use := range info.Uses {
			if use != obj {
				continue
			}
			p := id.Pos()
			if p > blockEnd && p < enclosing.Body.Rbrace {
				return true
			}
		}
		return false
	}

	for obj := range declaredInBlock {
		if _, isVar := obj.(*types.Var); !isVar {
			continue
		}
		if usedAfterBlock(obj) {
			returns = append(returns, paramSpec{
				name:   obj.Name(),
				typeS:  types.TypeString(obj.Type(), relativeToPkg(pkg.Types)),
				object: obj,
			})
		}
	}
	_ = blockStart
	return params, returns, nil
}
