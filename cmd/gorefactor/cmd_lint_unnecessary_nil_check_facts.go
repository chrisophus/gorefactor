package main

// Fact bookkeeping and expression checks for the unnecessary-nil-check rule;
// the statement walk lives in cmd_lint_unnecessary_nil_check.go.

import (
	"fmt"
	"go/ast"
	"go/token"
)

// invalidateNilFacts drops flow facts for every name node could have changed:
// assignment/inc-dec targets, redeclarations (which also shadow, so the kind
// is cleared), range variables, and receivers of method calls when the
// receiver kind could let the method mutate the variable itself (a named
// slice/map — or unknown — can have pointer-receiver methods that assign
// through the implicit &x; a *T or interface receiver cannot rebind x).
func invalidateNilFacts(node ast.Node, vars map[string]nilVarState) {
	if node == nil {
		return
	}
	clear := func(name string, shadow bool) {
		v, ok := vars[name]
		if !ok {
			return
		}
		v.fact = factNone
		if shadow {
			v.kind = ""
		}
		vars[name] = v
	}
	ast.Inspect(node, func(n ast.Node) bool {
		switch st := n.(type) {
		case *ast.AssignStmt:
			for _, lhs := range st.Lhs {
				if id, ok := lhs.(*ast.Ident); ok {
					clear(id.Name, st.Tok == token.DEFINE)
				}
			}
		case *ast.IncDecStmt:
			if id, ok := st.X.(*ast.Ident); ok {
				clear(id.Name, false)
			}
		case *ast.ValueSpec:
			for _, id := range st.Names {
				clear(id.Name, true)
			}
		case *ast.RangeStmt:
			for _, e := range []ast.Expr{st.Key, st.Value} {
				if id, ok := e.(*ast.Ident); ok {
					clear(id.Name, st.Tok == token.DEFINE)
				}
			}
		case *ast.UnaryExpr:
			if st.Op == token.AND {
				if id, ok := stripParens(st.X).(*ast.Ident); ok {
					clear(id.Name, false)
				}
			}
		case *ast.CallExpr:
			if sel, ok := st.Fun.(*ast.SelectorExpr); ok {
				if id, ok := stripParens(sel.X).(*ast.Ident); ok {
					if k := vars[id.Name].kind; k != kindPointer && k != kindIface {
						clear(id.Name, false)
					}
				}
			}
		}
		return true
	})
}

func (s *nilCheckScanner) recordNilAssign(st *ast.AssignStmt, vars map[string]nilVarState) {
	pairwise := len(st.Lhs) == len(st.Rhs)
	for i, lhs := range st.Lhs {
		id, ok := lhs.(*ast.Ident)
		if !ok || id.Name == "_" {
			continue
		}
		prev := vars[id.Name]
		state := nilVarState{local: prev.local || st.Tok == token.DEFINE}
		if st.Tok == token.ASSIGN {
			state.kind = prev.kind // plain assignment cannot change the type
		}
		if pairwise {
			kind, nonNil := constructedNonNil(st.Rhs[i])
			if kind != "" {
				state.kind = kind
			}
			if nonNil && state.local && !s.tainted[id.Name] {
				state.fact = factConstructed
				state.factLine = s.line(st)
			}
		}
		vars[id.Name] = state
	}
}

func (s *nilCheckScanner) recordNilDecl(st *ast.DeclStmt, vars map[string]nilVarState) {
	gen, ok := st.Decl.(*ast.GenDecl)
	if !ok || gen.Tok != token.VAR {
		return
	}
	for _, spec := range gen.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		var declaredKind string
		if vs.Type != nil {
			declaredKind = nilCheckTypeKind(vs.Type)
		}
		for i, id := range vs.Names {
			if id == nil || id.Name == "_" {
				continue
			}
			state := nilVarState{local: true, kind: declaredKind}
			if vs.Type == nil && len(vs.Values) == len(vs.Names) {
				kind, nonNil := constructedNonNil(vs.Values[i])
				state.kind = kind
				if nonNil && !s.tainted[id.Name] {
					state.fact = factConstructed
					state.factLine = s.line(st)
				}
			}
			vars[id.Name] = state
		}
	}
}

// constructedNonNil reports whether expr syntactically constructs a value
// that cannot be nil, and the nilable kind it constructs (if recognizable).
func constructedNonNil(expr ast.Expr) (kind string, nonNil bool) {
	switch e := stripParens(expr).(type) {
	case *ast.UnaryExpr:
		if e.Op == token.AND {
			return kindPointer, true
		}
	case *ast.CallExpr:
		id, ok := e.Fun.(*ast.Ident)
		if !ok {
			return "", false
		}
		switch id.Name {
		case "new":
			return kindPointer, true
		case "make":
			if len(e.Args) > 0 {
				return nilCheckTypeKind(e.Args[0]), true
			}
		}
	case *ast.CompositeLit:
		switch t := e.Type.(type) {
		case *ast.ArrayType:
			if t.Len == nil {
				return kindSlice, true
			}
		case *ast.MapType:
			return kindMap, true
		}
	}
	return "", false
}

func (s *nilCheckScanner) checkExprs(exprs []ast.Expr, vars map[string]nilVarState) {
	for _, e := range exprs {
		s.checkExpr(e, vars)
	}
}

func (s *nilCheckScanner) checkExpr(e ast.Expr, vars map[string]nilVarState) {
	if e == nil {
		return
	}
	ast.Inspect(e, func(n ast.Node) bool {
		be, ok := n.(*ast.BinaryExpr)
		if !ok {
			return true
		}
		switch be.Op {
		case token.LAND, token.LOR:
			s.checkLenCombo(be, vars)
		case token.EQL, token.NEQ:
			s.checkNilCompare(be, vars)
		}
		return true
	})
}

func (s *nilCheckScanner) checkNilCompare(be *ast.BinaryExpr, vars map[string]nilVarState) {
	if s.consumed[be] {
		return
	}
	name, ok := identNilCompare(be, be.Op)
	if !ok {
		return
	}
	v := vars[name]
	if v.fact == factNone || s.tainted[name] {
		return
	}
	s.consumed[be] = true
	verdict := "can never be true"
	if be.Op == token.NEQ {
		verdict = "is always true"
	}
	var reason string
	switch v.fact {
	case factConstructed:
		reason = fmt.Sprintf("%s was assigned a non-nil value at line %d", name, v.factLine)
	case factGuarded:
		reason = fmt.Sprintf("the branch condition at line %d already establishes %s != nil", v.factLine, name)
	case factRejected:
		reason = fmt.Sprintf("the guard at line %d already returned on nil", v.factLine)
	}
	op := "=="
	if be.Op == token.NEQ {
		op = "!="
	}
	s.issues = append(s.issues, lintIssue{
		File:     s.file,
		Rule:     "unnecessary-nil-check",
		Severity: "warning",
		Message: fmt.Sprintf("func %s: `%s %s nil` (line %d) %s — %s; drop the redundant check",
			s.fn, name, op, s.line(be), verdict, reason),
	})
}

// checkLenCombo flags `x != nil && len(x) > 0` (and `x == nil || len(x) == 0`)
// for slice/map-typed x: len of a nil slice or map is 0, so the len test alone
// is equivalent. The nil-compare node is marked consumed so the fact-based
// check does not double-report the same site.
func (s *nilCheckScanner) checkLenCombo(be *ast.BinaryExpr, vars map[string]nilVarState) {
	compareOp, lenSuffices := token.NEQ, "> 0"
	lenMatches := lenComparePositive
	if be.Op == token.LOR {
		compareOp, lenSuffices = token.EQL, "== 0"
		lenMatches = lenCompareZero
	}
	terms := flattenBool(be, be.Op)
	for _, t := range terms {
		cmp, ok := stripParens(t).(*ast.BinaryExpr)
		if !ok || s.consumed[cmp] {
			continue
		}
		name, ok := identNilCompare(cmp, compareOp)
		if !ok {
			continue
		}
		if k := vars[name].kind; k != kindSlice && k != kindMap {
			continue
		}
		for _, other := range terms {
			if other == t || !lenMatches(other, name) {
				continue
			}
			s.consumed[cmp] = true
			op := "!="
			if compareOp == token.EQL {
				op = "=="
			}
			s.issues = append(s.issues, lintIssue{
				File:     s.file,
				Rule:     "unnecessary-nil-check",
				Severity: "warning",
				Message: fmt.Sprintf("func %s: `%s %s nil` (line %d) is redundant — len(nil) == 0, so `len(%s) %s` alone suffices",
					s.fn, name, op, s.line(cmp), name, lenSuffices),
			})
			break
		}
	}
}

// lenComparePositive matches len(name) > 0, len(name) != 0, len(name) >= 1
// (and the mirrored literal-first spellings).
func lenComparePositive(e ast.Expr, name string) bool {
	op, lit, ok := lenCompareParts(e, name)
	if !ok {
		return false
	}
	return (op == token.GTR && lit == 0) || (op == token.NEQ && lit == 0) || (op == token.GEQ && lit == 1)
}

// lenCompareZero matches len(name) == 0, len(name) < 1, len(name) <= 0
// (and the mirrored literal-first spellings).
func lenCompareZero(e ast.Expr, name string) bool {
	op, lit, ok := lenCompareParts(e, name)
	if !ok {
		return false
	}
	return (op == token.EQL && lit == 0) || (op == token.LSS && lit == 1) || (op == token.LEQ && lit == 0)
}

// lenCompareParts normalizes a comparison between len(name) and an integer
// literal to the len-on-the-left orientation, returning the operator and the
// literal value.
func lenCompareParts(e ast.Expr, name string) (token.Token, int, bool) {
	be, ok := stripParens(e).(*ast.BinaryExpr)
	if !ok {
		return 0, 0, false
	}
	op := be.Op
	lhs, rhs := stripParens(be.X), stripParens(be.Y)
	if !isLenOf(lhs, name) {
		lhs, rhs = rhs, lhs
		switch op {
		case token.GTR:
			op = token.LSS
		case token.LSS:
			op = token.GTR
		case token.GEQ:
			op = token.LEQ
		case token.LEQ:
			op = token.GEQ
		}
	}
	if !isLenOf(lhs, name) {
		return 0, 0, false
	}
	lit, ok := rhs.(*ast.BasicLit)
	if !ok || lit.Kind != token.INT {
		return 0, 0, false
	}
	switch lit.Value {
	case "0":
		return op, 0, true
	case "1":
		return op, 1, true
	}
	return 0, 0, false
}

func isLenOf(e ast.Expr, name string) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return false
	}
	fun, ok := call.Fun.(*ast.Ident)
	if !ok || fun.Name != "len" {
		return false
	}
	id, ok := stripParens(call.Args[0]).(*ast.Ident)
	return ok && id.Name == name
}
