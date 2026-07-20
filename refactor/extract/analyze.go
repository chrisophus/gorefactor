package extract

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"sort"

	"golang.org/x/tools/go/packages"
)

// paramSpec describes one inferred parameter or return value of the extracted
// block.
type paramSpec struct {
	name   string
	typeS  string
	object types.Object
	outer  bool // a pre-existing variable the block mutates (write-back at call site with =, not :=)
}

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
			// Type-switch case variables (`switch s := x.(type)`) are recorded
			// as implicit objects per case clause, not in Defs. Without this
			// they look free and get wrongly lifted to parameters, producing
			// `undefined: s` at the call site.
			if obj := info.Implicits[n]; obj != nil {
				declaredInBlock[obj] = true
			}
			return true
		})
	}

	blockEnd := stmts[len(stmts)-1].End()

	isCandidateVar := func(obj types.Object) bool {
		if obj == nil || declaredInBlock[obj] {
			return false
		}
		if _, isVar := obj.(*types.Var); !isVar {
			return false
		}
		if obj.Pkg() == nil || obj.Pkg().Path() != pkg.PkgPath {
			return false
		}
		return isLocalToFunc(obj, enclosing, info)
	}

	seenRead := map[types.Object]bool{}
	for _, stmt := range stmts {
		ast.Inspect(stmt, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok {
				return true
			}
			obj := info.Uses[id]
			if !isCandidateVar(obj) || seenRead[obj] {
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

	// Outer variables the block assigns through a pure value path lose the
	// mutation when they become by-value parameters, so any of them still used
	// after the block must be returned and written back at the call site.
	// (Writes that reach shared memory — through a pointer, slice, or map —
	// survive the copy and need no write-back.)
	mutatedOuter := map[types.Object]bool{}
	markRoot := func(e ast.Expr) {
		root := lhsRootIfValuePath(info, e)
		if root == nil {
			return
		}
		if obj := info.Uses[root]; isCandidateVar(obj) {
			mutatedOuter[obj] = true
		}
	}
	processStmts(stmts, markRoot)

	appendReturn := func(obj types.Object, outer bool) {
		returns = append(returns, paramSpec{
			name:   obj.Name(),
			typeS:  types.TypeString(obj.Type(), relativeToPkg(pkg.Types)),
			object: obj,
			outer:  outer,
		})
	}
	for obj := range declaredInBlock {
		if _, isVar := obj.(*types.Var); !isVar {
			continue
		}
		if usedAfterBlock(obj) {
			appendReturn(obj, false)
		}
	}
	for obj := range mutatedOuter {
		if usedAfterBlock(obj) {
			appendReturn(obj, true)
		}
	}
	// Map iteration above is unordered; sort by declaration position so the
	// generated signature and call site are deterministic across runs.
	sort.Slice(returns, func(i, j int) bool { return returns[i].object.Pos() < returns[j].object.Pos() })
	return params, returns, nil
}

func processStmts(stmts []ast.Stmt, markRoot func(e ast.Expr)) {
	for _, stmt := range stmts {
		ast.Inspect(stmt, func(n ast.Node) bool {
			switch s := n.(type) {
			case *ast.AssignStmt:
				for _, lhs := range s.Lhs {
					markRoot(lhs)
				}
			case *ast.IncDecStmt:
				markRoot(s.X)
			case *ast.RangeStmt:
				if s.Tok == token.ASSIGN {
					if s.Key != nil {
						markRoot(s.Key)
					}
					if s.Value != nil {
						markRoot(s.Value)
					}
				}
			}
			return true
		})
	}
}

func lhsRootIfValuePath(info *types.Info, e ast.Expr) *ast.Ident {
	for {
		switch v := e.(type) {
		case *ast.Ident:
			return v
		case *ast.ParenExpr:
			e = v.X
		case *ast.SelectorExpr:
			if t, ok := info.Types[v.X]; ok && t.Type != nil {
				if _, isPtr := t.Type.Underlying().(*types.Pointer); isPtr {
					return nil
				}
			}
			e = v.X
		case *ast.IndexExpr:
			if t, ok := info.Types[v.X]; ok && t.Type != nil {
				switch t.Type.Underlying().(type) {
				case *types.Slice, *types.Map, *types.Pointer:
					return nil
				}
			}
			e = v.X
		case *ast.StarExpr:
			return nil
		default:
			return nil
		}
	}
}
