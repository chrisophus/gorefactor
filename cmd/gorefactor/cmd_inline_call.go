package main

import (
	"go/ast"
	"go/token"
)

// callSitesInFile scans one same-package file for call sites of funcName.
// localDecl is the target's FuncDecl as parsed in this file's AST (nil when
// the declaration lives in another file of the package).
func callSitesInFile(fset *token.FileSet, node *ast.File, src []byte, f, funcName string, hasResults bool, paramCount int) ([]inlineCallSite, error) {
	var localDecl *ast.FuncDecl
	for _, d := range node.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && fd.Name.Name == funcName {
			localDecl = fd
			break
		}
	}
	if shadowLine := findShadowingDecl(fset, node, funcName, localDecl); shadowLine != "" {
		return nil, parseErrorf("cannot inline %s: name is redeclared or shadowed at %s — refusing (cannot distinguish uses)", funcName, shadowLine)
	}

	callFun := map[*ast.Ident]*ast.CallExpr{}
	stmtOf := map[*ast.CallExpr]*ast.ExprStmt{}
	skip := map[*ast.Ident]bool{}
	ast.Inspect(node, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.CallExpr:
			if id, ok := v.Fun.(*ast.Ident); ok {
				callFun[id] = v
			}
		case *ast.SelectorExpr:
			skip[v.Sel] = true
		case *ast.BlockStmt:
			for _, s := range v.List {
				if es, ok := s.(*ast.ExprStmt); ok {
					if c, ok := es.X.(*ast.CallExpr); ok {
						stmtOf[c] = es
					}
				}
			}
		case *ast.CaseClause:
			for _, s := range v.Body {
				if es, ok := s.(*ast.ExprStmt); ok {
					if c, ok := es.X.(*ast.CallExpr); ok {
						stmtOf[c] = es
					}
				}
			}
		case *ast.CommClause:
			for _, s := range v.Body {
				if es, ok := s.(*ast.ExprStmt); ok {
					if c, ok := es.X.(*ast.CallExpr); ok {
						stmtOf[c] = es
					}
				}
			}
		}
		return true
	})

	var sites []inlineCallSite
	var refusal error
	sites, refusal = extractBlockL64(node, funcName, skip, refusal, localDecl, fset, callFun, f, paramCount, src, stmtOf, sites)
	if refusal != nil {
		return nil, refusal
	}

	// Statement-mode bodies require every call to sit in statement position
	// directly inside a block or case body.
	for _, s := range sites {
		if !hasResults && s.stmtStart < 0 {
			return nil, parseErrorf("cannot inline %s: call at %s:%d is not in statement position", funcName, f, s.line)
		}
	}
	return sites, nil
}

func extractBlockL64(node *ast.File, funcName string, skip map[*ast.Ident]bool, refusal error, localDecl *ast.FuncDecl, fset *token.FileSet, callFun map[*ast.Ident]*ast.CallExpr, f string, paramCount int, src []byte, stmtOf map[*ast.CallExpr]*ast.ExprStmt, sites []inlineCallSite) ([]inlineCallSite, error) {
	ast.Inspect(node, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok || id.Name != funcName || skip[id] || refusal != nil {
			return true
		}
		if localDecl != nil && id == localDecl.Name {
			return true
		}
		pos := fset.Position(id.Pos())
		call, isCall := callFun[id]
		if !isCall {
			refusal = parseErrorf("cannot inline %s: used as a value (not called) at %s:%d", funcName, f, pos.Line)
			return true
		}
		if call.Ellipsis.IsValid() {
			refusal = parseErrorf("cannot inline %s: call at %s:%d uses ... expansion", funcName, f, pos.Line)
			return true
		}
		if len(call.Args) != paramCount {
			refusal = parseErrorf("cannot inline %s: call at %s:%d passes %d arg(s), function has %d parameter(s)", funcName, f, pos.Line, len(call.Args), paramCount)
			return true
		}
		site := inlineCallSite{
			file:      f,
			src:       src,
			line:      pos.Line,
			call:      call,
			start:     fset.Position(call.Pos()).Offset,
			end:       fset.Position(call.End()).Offset,
			stmtStart: -1,
			stmtEnd:   -1,
		}
		if es, ok := stmtOf[call]; ok {
			site.stmtStart = fset.Position(es.Pos()).Offset
			site.stmtEnd = fset.Position(es.End()).Offset
		}
		for _, a := range call.Args {
			site.argStart = append(site.argStart, fset.Position(a.Pos()).Offset)
			site.argEnd = append(site.argEnd, fset.Position(a.End()).Offset)
		}
		sites = append(sites, site)
		return true
	})
	return sites, refusal
}
