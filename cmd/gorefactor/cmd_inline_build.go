package main

import (
	"go/ast"
	"go/token"
	"sort"
)

// buildInlineTemplate validates the function shape and extracts the
// substitution template. All refusals are specific exit-3 errors.
func buildInlineTemplate(fset *token.FileSet, src []byte, fd *ast.FuncDecl) (*inlineTemplate, error) {
	name := fd.Name.Name
	if fd.Body == nil {
		return nil, parseErrorf("cannot inline %s: function has no body", name)
	}
	if fd.Type.TypeParams != nil && len(fd.Type.TypeParams.List) > 0 {
		return nil, parseErrorf("cannot inline %s: generic functions are not supported", name)
	}
	params, err := flattenParamNames(fd, name)
	if err != nil {
		return nil, err
	}
	if err := refuseComplexBody(fd, name); err != nil {
		return nil, err
	}

	tmpl := &inlineTemplate{params: params}
	region, err := inlineDetermineRegion(fd, tmpl, name)
	if err != nil {
		return nil, err
	}

	regStart := fset.Position(region.Pos()).Offset
	regEnd := fset.Position(region.End()).Offset
	tmpl.body = string(src[regStart:regEnd])

	paramSet := map[string]int{}
	for i, p := range params {
		paramSet[p] = i
	}
	counts := make([]int, len(params))
	var walkErr error
	skip := selectorAndKeyIdents(regionAST(fd, tmpl.exprMode), paramSet, &walkErr)
	if walkErr != nil {
		return nil, walkErr
	}
	extractBlockL77(fd, tmpl, skip, paramSet, counts, fset, regStart)
	for i, c := range counts {
		if c > 1 {
			return nil, parseErrorf("cannot inline %s: parameter %q is used %d times; temp vars are out of scope — refusing", name, params[i], c)
		}
	}
	// Taking a parameter's address aliases the caller's variable after
	// substitution — observable semantic change.
	var addrErr error
	ast.Inspect(regionAST(fd, tmpl.exprMode), func(n ast.Node) bool {
		if u, ok := n.(*ast.UnaryExpr); ok && u.Op == token.AND {
			if id, ok := u.X.(*ast.Ident); ok {
				if _, isParam := paramSet[id.Name]; isParam && addrErr == nil {
					addrErr = parseErrorf("cannot inline %s: body takes the address of parameter %q", name, id.Name)
				}
			}
		}
		return addrErr == nil
	})
	if addrErr != nil {
		return nil, addrErr
	}
	sort.Slice(tmpl.uses, func(i, j int) bool { return tmpl.uses[i].start < tmpl.uses[j].start })
	return tmpl, nil

}

func inlineDetermineRegion(fd *ast.FuncDecl, tmpl *inlineTemplate, name string) (ast.Node, error) {
	if len(fd.Body.List) == 1 {
		if ret, ok := fd.Body.List[0].(*ast.ReturnStmt); ok {
			if len(ret.Results) != 1 {
				return nil, parseErrorf("cannot inline %s: only single-value returns are supported (got %d results)", name, len(ret.Results))
			}
			tmpl.exprMode = true
			tmpl.returnExpr = ret.Results[0]
			return ret.Results[0], nil
		}
	}

	hasReturn := false
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		if _, ok := n.(*ast.ReturnStmt); ok {
			hasReturn = true
		}
		return true
	})
	if hasReturn {
		return nil, parseErrorf("cannot inline %s: bodies with return statements are only supported as a single `return <expr>`", name)
	}
	if fd.Type.Results != nil && len(fd.Type.Results.List) > 0 {
		return nil, parseErrorf("cannot inline %s: function declares results but has no single return", name)
	}
	if err := refuseStmtModeHazards(fd, name); err != nil {
		return nil, err
	}
	if len(fd.Body.List) == 0 {
		return nil, parseErrorf("cannot inline %s: empty body (use delete --safe instead)", name)
	}
	return bodyRegion{fd.Body.List[0].Pos(), fd.Body.List[len(fd.Body.List)-1].End()}, nil
}

func extractBlockL77(fd *ast.FuncDecl, tmpl *inlineTemplate, skip map[*ast.Ident]bool, paramSet map[string]int, counts []int, fset *token.FileSet, regStart int) {
	ast.Inspect(regionAST(fd, tmpl.exprMode), func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok || skip[id] {
			return true
		}
		idx, isParam := paramSet[id.Name]
		if !isParam {
			return true
		}
		counts[idx]++
		tmpl.uses = append(tmpl.uses, paramUse{
			start: fset.Position(id.Pos()).Offset - regStart,
			end:   fset.Position(id.End()).Offset - regStart,
			param: idx,
		})
		return true
	})
}
