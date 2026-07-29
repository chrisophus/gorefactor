package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// unnecessary-nil-check flags nil checks that are provably excessive within a
// single function body — no cross-function reasoning, no type checking:
//
//   - a check on a variable just assigned a value that cannot be nil
//     (&x, new(T), make(...), or a slice/map composite literal);
//   - a duplicate guard: `if x == nil { return }` followed by another nil
//     check of x with no intervening reassignment;
//   - a check nested inside a branch that already established `x != nil`
//     (or the else branch of an `x == nil` test);
//   - `x != nil && len(x) > 0` (or `x == nil || len(x) == 0`) where x is a
//     slice or map — len(nil) == 0, so the nil test adds nothing.
//
// The rule under-reports by design. Non-nil facts are only tracked for local
// variables (params, receivers, := / var declarations), are dropped on any
// reassignment or on a method call that could mutate the value through an
// addressable receiver, and are never established for variables whose address
// is taken or that are assigned inside a closure. Loop bodies start with no
// facts (an assignment later in the body reaches an earlier check on the next
// iteration), and functions containing goto or labels are skipped entirely.

type unnecessaryNilCheckRule struct{}

func (unnecessaryNilCheckRule) Name() string { return "unnecessary-nil-check" }

func (r unnecessaryNilCheckRule) Run(ctx LintContext) []lintIssue {
	var out []lintIssue
	for _, file := range ctx.Files {
		if isTestFile(file) {
			continue
		}
		fset := token.NewFileSet()
		astFile, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			continue
		}
		for _, decl := range astFile.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			out = append(out, unnecessaryNilCheckFunc(file, fset, fd)...)
		}
	}
	return out
}

type nilFactKind int

const (
	factNone nilFactKind = iota
	factConstructed
	factGuarded
	factRejected
)

const (
	kindPointer = "pointer"
	kindIface   = "interface"
	kindSlice   = "slice"
	kindMap     = "map"
)

// nilVarState is the per-scope knowledge about one variable. kind is a static
// type property (survives plain assignment, cleared by shadowing); fact is a
// flow fact (cleared by anything that could change the value).
type nilVarState struct {
	local    bool
	kind     string
	fact     nilFactKind
	factLine int
}

type nilCheckScanner struct {
	file    string
	fset    *token.FileSet
	fn      string
	tainted map[string]bool
	// consumed marks nil-compare nodes already reported (or folded into a
	// len-combo report) so one site never yields two findings.
	consumed map[ast.Expr]bool
	issues   []lintIssue
}

func unnecessaryNilCheckFunc(file string, fset *token.FileSet, fd *ast.FuncDecl) []lintIssue {
	if hasGotoOrLabel(fd.Body) {
		return nil
	}
	s := &nilCheckScanner{
		file:     file,
		fset:     fset,
		fn:       fd.Name.Name,
		tainted:  closureTaintedNames(fd.Body),
		consumed: map[ast.Expr]bool{},
	}
	vars := map[string]nilVarState{}
	addNilCheckParams(vars, fd.Recv)
	addNilCheckParams(vars, fd.Type.Params)
	addNilCheckParams(vars, fd.Type.Results)
	s.walkStmts(fd.Body.List, vars)
	return s.issues
}

func addNilCheckParams(vars map[string]nilVarState, fl *ast.FieldList) {
	if fl == nil {
		return
	}
	for _, f := range fl.List {
		kind := nilCheckTypeKind(f.Type)
		for _, n := range f.Names {
			if n == nil || n.Name == "" || n.Name == "_" {
				continue
			}
			vars[n.Name] = nilVarState{local: true, kind: kind}
		}
	}
}

func nilCheckTypeKind(typ ast.Expr) string {
	switch t := typ.(type) {
	case *ast.ArrayType:
		if t.Len == nil {
			return kindSlice
		}
		return ""
	case *ast.MapType:
		return kindMap
	}
	return nilableKind(typ) // "pointer", "interface", or ""
}

func hasGotoOrLabel(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		switch b := n.(type) {
		case *ast.LabeledStmt:
			found = true
		case *ast.BranchStmt:
			if b.Tok == token.GOTO {
				found = true
			}
		}
		return !found
	})
	return found
}

// closureTaintedNames collects names that can change behind the scanner's
// back at any point: names assigned inside a function literal (the closure
// may run between a fact and a later check) and names whose address is taken
// anywhere in the body. Facts are never established for them.
func closureTaintedNames(body *ast.BlockStmt) map[string]bool {
	t := map[string]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.UnaryExpr:
			if v.Op == token.AND {
				if id, ok := stripParens(v.X).(*ast.Ident); ok {
					t[id.Name] = true
				}
			}
		case *ast.FuncLit:
			ast.Inspect(v.Body, func(m ast.Node) bool {
				switch w := m.(type) {
				case *ast.AssignStmt:
					for _, lhs := range w.Lhs {
						if id, ok := lhs.(*ast.Ident); ok {
							t[id.Name] = true
						}
					}
				case *ast.IncDecStmt:
					if id, ok := w.X.(*ast.Ident); ok {
						t[id.Name] = true
					}
				}
				return true
			})
		}
		return true
	})
	return t
}

func copyNilVars(vars map[string]nilVarState) map[string]nilVarState {
	out := make(map[string]nilVarState, len(vars))
	for k, v := range vars {
		out[k] = v
	}
	return out
}

func stripNilFacts(vars map[string]nilVarState) map[string]nilVarState {
	for k, v := range vars {
		v.fact = factNone
		vars[k] = v
	}
	return vars
}

func (s *nilCheckScanner) line(n ast.Node) int { return s.fset.Position(n.Pos()).Line }

func (s *nilCheckScanner) walkStmts(list []ast.Stmt, vars map[string]nilVarState) {
	for _, stmt := range list {
		s.walkStmt(stmt, vars)
	}
}

func (s *nilCheckScanner) walkStmt(stmt ast.Stmt, vars map[string]nilVarState) {
	switch st := stmt.(type) {
	case *ast.AssignStmt:
		s.checkExprs(st.Rhs, vars)
		invalidateNilFacts(st, vars)
		s.recordNilAssign(st, vars)
	case *ast.DeclStmt:
		invalidateNilFacts(st, vars)
		s.recordNilDecl(st, vars)
	case *ast.IfStmt:
		s.walkIf(st, vars)
	case *ast.BlockStmt:
		s.walkStmts(st.List, copyNilVars(vars))
		invalidateNilFacts(st, vars)
	case *ast.ForStmt:
		inner := copyNilVars(vars)
		if st.Init != nil {
			s.walkStmt(st.Init, inner)
		}
		s.walkStmts(st.Body.List, stripNilFacts(inner))
		invalidateNilFacts(st, vars)
	case *ast.RangeStmt:
		s.checkExpr(st.X, vars)
		inner := copyNilVars(vars)
		if st.Tok == token.DEFINE {
			for _, e := range []ast.Expr{st.Key, st.Value} {
				if id, ok := e.(*ast.Ident); ok && id.Name != "_" {
					inner[id.Name] = nilVarState{local: true}
				}
			}
		}
		s.walkStmts(st.Body.List, stripNilFacts(inner))
		invalidateNilFacts(st, vars)
	case *ast.SwitchStmt:
		s.walkSwitch(st, vars)
	case *ast.TypeSwitchStmt:
		scope := copyNilVars(vars)
		if st.Init != nil {
			s.walkStmt(st.Init, scope)
		}
		invalidateNilFacts(st.Assign, scope)
		for _, c := range st.Body.List {
			if cc, ok := c.(*ast.CaseClause); ok {
				s.walkStmts(cc.Body, copyNilVars(scope))
			}
		}
		invalidateNilFacts(st, vars)
	case *ast.SelectStmt:
		for _, c := range st.Body.List {
			if cc, ok := c.(*ast.CommClause); ok {
				scope := copyNilVars(vars)
				if cc.Comm != nil {
					s.walkStmt(cc.Comm, scope)
				}
				s.walkStmts(cc.Body, scope)
			}
		}
		invalidateNilFacts(st, vars)
	case *ast.ReturnStmt:
		s.checkExprs(st.Results, vars)
		invalidateNilFacts(st, vars)
	case *ast.ExprStmt:
		s.checkExpr(st.X, vars)
		invalidateNilFacts(st, vars)
	default:
		invalidateNilFacts(stmt, vars)
	}
}

func (s *nilCheckScanner) walkSwitch(st *ast.SwitchStmt, vars map[string]nilVarState) {
	scope := copyNilVars(vars)
	if st.Init != nil {
		s.walkStmt(st.Init, scope)
	}
	if st.Tag != nil {
		s.checkExpr(st.Tag, scope)
	}
	fall := false
	ast.Inspect(st.Body, func(n ast.Node) bool {
		if b, ok := n.(*ast.BranchStmt); ok && b.Tok == token.FALLTHROUGH {
			fall = true
		}
		return !fall
	})
	for _, c := range st.Body.List {
		cc, ok := c.(*ast.CaseClause)
		if !ok {
			continue
		}
		if st.Tag == nil {
			s.checkExprs(cc.List, scope)
		}
		body := copyNilVars(scope)
		if fall {
			// A fallthrough lets one case's assignments reach the next
			// case's checks; drop flow facts for every body.
			body = stripNilFacts(body)
		}
		s.walkStmts(cc.Body, body)
	}
	invalidateNilFacts(st, vars)
}

func (s *nilCheckScanner) walkIf(st *ast.IfStmt, vars map[string]nilVarState) {
	scope := vars
	if st.Init != nil {
		scope = copyNilVars(vars)
		s.walkStmt(st.Init, scope)
	}
	s.checkExpr(st.Cond, scope)
	invalidateNilFacts(st.Cond, scope)

	bodyVars := copyNilVars(scope)
	s.establishFacts(bodyVars, nonNilConjunctNames(st.Cond), factGuarded, s.line(st.Cond))
	s.walkStmts(st.Body.List, bodyVars)

	if st.Else != nil {
		elseVars := copyNilVars(scope)
		// In the else branch the condition is false, so every `x == nil`
		// disjunct is false: those names are non-nil.
		s.establishFacts(elseVars, nilEqDisjunctNames(st.Cond, false), factGuarded, s.line(st.Cond))
		s.walkStmt(st.Else, elseVars)
	}

	invalidateNilFacts(st, vars)

	// Fall-through past `if x == nil { return }` (no else, terminal body)
	// proves x non-nil for the rest of this statement list. Skipped when the
	// if declares its own variables — the guard may cover a shadowed x.
	if st.Else == nil && st.Init == nil && terminatesFlow(st.Body) {
		s.establishFacts(vars, nilEqDisjunctNames(st.Cond, false), factRejected, s.line(st.Cond))
	}
}

func (s *nilCheckScanner) establishFacts(vars map[string]nilVarState, names []string, fact nilFactKind, line int) {
	for _, name := range names {
		v, ok := vars[name]
		if !ok || !v.local || s.tainted[name] || v.fact != factNone {
			continue
		}
		v.fact = fact
		v.factLine = line
		vars[name] = v
	}
}

func terminatesFlow(body *ast.BlockStmt) bool {
	if body == nil || len(body.List) == 0 {
		return false
	}
	switch last := body.List[len(body.List)-1].(type) {
	case *ast.ReturnStmt:
		return true
	case *ast.BranchStmt:
		return last.Tok == token.BREAK || last.Tok == token.CONTINUE
	case *ast.ExprStmt:
		call, ok := last.X.(*ast.CallExpr)
		if !ok {
			return false
		}
		switch fun := call.Fun.(type) {
		case *ast.Ident:
			return fun.Name == "panic"
		case *ast.SelectorExpr:
			if x, ok := fun.X.(*ast.Ident); ok {
				return (x.Name == "os" && fun.Sel.Name == "Exit") ||
					(x.Name == "log" && strings.HasPrefix(fun.Sel.Name, "Fatal"))
			}
		}
	}
	return false
}
