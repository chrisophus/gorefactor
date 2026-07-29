package main

// Caller-side non-nil proofs for the redundant-nil-guard rule: a guard is
// only redundant if every in-package call site provably passes a non-nil
// argument. The rule definition and call-site indexing live in
// cmd_lint_redundant_nil_guard.go.

import (
	"go/ast"
	"go/token"
	"slices"
)

func argProvenNonNil(fn *ast.FuncDecl, call *ast.CallExpr, arg ast.Expr) bool {
	if exprConstructsNonNil(arg) {
		return true
	}
	id, ok := arg.(*ast.Ident)
	if !ok {
		return false
	}
	// A variable whose address is taken or that a closure assigns can change
	// behind any proof's back — nothing below is trustworthy for it.
	if closureTaintedNames(fn.Body)[id.Name] {
		return false
	}
	writes := writePositions(fn.Body, id.Name)
	// A call inside a func literal runs at an unknown time; if the variable
	// is ever written, any textual ordering argument is void.
	if len(writes) > 0 && callInsideFuncLit(fn.Body, call) {
		return false
	}
	if localAssignedNonNil(fn.Body, call, id.Name) {
		return true
	}
	if enclosingNonNilGuard(fn.Body, call, id.Name, writes) {
		return true
	}
	return precedingNilReject(fn.Body, call, id.Name, writes)
}

// writePositions returns the positions where name is a write target in body:
// an assignment or inc/dec target, a range variable, or a var
// (re)declaration. Guard-based proofs only hold while the guarded value
// cannot have changed since the guard, so a write inside the proof-relevant
// span voids the proof: `if t == nil { return }; t = derive(t); use(t)` may
// well pass nil.
func writePositions(body *ast.BlockStmt, name string) []token.Pos {
	var writes []token.Pos
	ast.Inspect(body, func(n ast.Node) bool {
		switch st := n.(type) {
		case *ast.AssignStmt:
			for _, lhs := range st.Lhs {
				if id, ok := lhs.(*ast.Ident); ok && id.Name == name {
					writes = append(writes, id.Pos())
				}
			}
		case *ast.IncDecStmt:
			if id, ok := st.X.(*ast.Ident); ok && id.Name == name {
				writes = append(writes, id.Pos())
			}
		case *ast.RangeStmt:
			for _, e := range []ast.Expr{st.Key, st.Value} {
				if id, ok := e.(*ast.Ident); ok && id.Name == name {
					writes = append(writes, id.Pos())
				}
			}
		case *ast.ValueSpec:
			for _, id := range st.Names {
				if id != nil && id.Name == name {
					writes = append(writes, id.Pos())
				}
			}
		}
		return true
	})
	return writes
}

func anyWriteIn(writes []token.Pos, lo, hi token.Pos) bool {
	for _, p := range writes {
		if p >= lo && p < hi {
			return true
		}
	}
	return false
}

func callInsideFuncLit(body *ast.BlockStmt, call *ast.CallExpr) bool {
	inside := false
	ast.Inspect(body, func(n ast.Node) bool {
		if fl, ok := n.(*ast.FuncLit); ok && nodeContains(fl, call) {
			inside = true
		}
		return !inside
	})
	return inside
}

func exprConstructsNonNil(arg ast.Expr) bool {
	switch a := arg.(type) {
	case *ast.UnaryExpr:
		return a.Op == token.AND
	case *ast.CallExpr:
		if id, ok := a.Fun.(*ast.Ident); ok && id.Name == "new" {
			return true
		}
	}
	return false
}

func localAssignedNonNil(body *ast.BlockStmt, call *ast.CallExpr, name string) bool {
	// Walk statement lists in order. On each assign/decl of name, record whether
	// the RHS constructs a non-nil value. Stop at the statement that contains
	// call (after scanning earlier siblings in nested blocks that contain call).
	// Two ways the textual order can lie are handled conservatively: a write
	// buried in an earlier sibling's branches may or may not run (drop the
	// fact), and a loop body that writes name re-enters its own top on the
	// next iteration (drop facts from before the loop on entry).
	var lastNonNil bool
	var seen bool
	var walk func([]ast.Stmt) bool // returns true when call was reached
	walk = func(list []ast.Stmt) bool {
		for _, stmt := range list {
			if nodeContains(stmt, call) {
				switch s := stmt.(type) {
				case *ast.IfStmt:
					if walk(s.Body.List) {
						return true
					}
					if s.Else != nil {
						if b, ok := s.Else.(*ast.BlockStmt); ok && walk(b.List) {
							return true
						}
					}
				case *ast.BlockStmt:
					return walk(s.List)
				case *ast.ForStmt:
					if nestedMaybeWrite(s.Body, name, true) {
						lastNonNil = false
					}
					return walk(s.Body.List)
				case *ast.RangeStmt:
					if nestedMaybeWrite(s.Body, name, true) || rangeWritesName(s, name) {
						lastNonNil = false
					}
					return walk(s.Body.List)
				}
				return true
			}
			if recorded, nonNil := recordedAssignOf(stmt, name); recorded {
				seen = true
				lastNonNil = nonNil
			} else if nestedMaybeWrite(stmt, name, false) {
				// A write to name hidden in this sibling's branches may or
				// may not have run by the time the call executes.
				seen = true
				lastNonNil = false
			}
		}
		return false
	}
	walk(body.List)
	return seen && lastNonNil
}

// recordedAssignOf reports whether stmt is a top-level assignment or var
// declaration writing name, and whether the value it assigns constructs a
// non-nil result. With several writes in one statement the last one wins.
func recordedAssignOf(stmt ast.Stmt, name string) (recorded, nonNil bool) {
	record := func(names []*ast.Ident, values []ast.Expr) {
		for i, n := range names {
			if n == nil || n.Name != name {
				continue
			}
			recorded = true
			nonNil = i < len(values) && exprConstructsNonNil(values[i])
		}
	}
	switch s := stmt.(type) {
	case *ast.AssignStmt:
		lhs := make([]*ast.Ident, len(s.Lhs))
		for i, e := range s.Lhs {
			lhs[i], _ = e.(*ast.Ident)
		}
		record(lhs, s.Rhs)
	case *ast.DeclStmt:
		gen, ok := s.Decl.(*ast.GenDecl)
		if !ok {
			return false, false
		}
		for _, spec := range gen.Specs {
			if vs, ok := spec.(*ast.ValueSpec); ok {
				record(vs.Names, vs.Values)
			}
		}
	}
	return recorded, nonNil
}

// nestedMaybeWrite reports whether node contains a write to name that
// localAssignedNonNil's ordered walk would not see: a plain assignment or
// inc/dec anywhere inside, plus — with includeDefine — `:=` declarations
// (needed for loop bodies, where even a shadowing declaration means the walk
// order no longer matches execution order across iterations).
func nestedMaybeWrite(node ast.Node, name string, includeDefine bool) bool {
	found := false
	ast.Inspect(node, func(n ast.Node) bool {
		switch st := n.(type) {
		case *ast.AssignStmt:
			if st.Tok == token.DEFINE && !includeDefine {
				return true
			}
			for _, lhs := range st.Lhs {
				if id, ok := lhs.(*ast.Ident); ok && id.Name == name {
					found = true
				}
			}
		case *ast.IncDecStmt:
			if id, ok := st.X.(*ast.Ident); ok && id.Name == name {
				found = true
			}
		case *ast.RangeStmt:
			if st.Tok != token.DEFINE || includeDefine {
				if rangeWritesName(st, name) {
					found = true
				}
			}
		}
		return !found
	})
	return found
}

func rangeWritesName(st *ast.RangeStmt, name string) bool {
	for _, e := range []ast.Expr{st.Key, st.Value} {
		if id, ok := e.(*ast.Ident); ok && id.Name == name {
			return true
		}
	}
	return false
}

// enclosingNonNilGuard reports whether call sits inside an `x != nil` branch
// for name. A guard whose body also writes name proves nothing — the write
// may run before the call directly or via a loop back-edge — so such guards
// are skipped, but an inner qualifying guard can still be found.
func enclosingNonNilGuard(body *ast.BlockStmt, call *ast.CallExpr, name string, writes []token.Pos) bool {
	var found bool
	ast.Inspect(body, func(n ast.Node) bool {
		if found || n == nil {
			return false
		}
		ifs, ok := n.(*ast.IfStmt)
		if !ok {
			return true
		}
		if slices.Contains(nonNilConjunctNames(ifs.Cond), name) && nodeContains(ifs.Body, call) &&
			!anyWriteIn(writes, ifs.Body.Pos(), ifs.Body.End()) {
			found = true
			return false
		}
		return true
	})
	return found
}

// precedingNilReject reports whether an earlier sibling `if x == nil { return }`
// guard precedes the statement containing call. A write to name between the
// guard and the call voids that guard (the value may have become nil again),
// but a later sibling guard can still qualify.
func precedingNilReject(body *ast.BlockStmt, call *ast.CallExpr, name string, writes []token.Pos) bool {
	var okProven bool
	var walkBlocks func(list []ast.Stmt)
	walkBlocks = func(list []ast.Stmt) {
		if okProven {
			return
		}
		for i, stmt := range list {
			if nodeContains(stmt, call) {
				for j := 0; j < i; j++ {
					ifs, ok := list[j].(*ast.IfStmt)
					if ok && slices.Contains(nilRejectGuardNames(ifs.Cond), name) && bodyStartsWithReturn(ifs.Body) &&
						!anyWriteIn(writes, ifs.End(), call.Pos()) {
						okProven = true
						return
					}
				}
			}
			ast.Inspect(stmt, func(n ast.Node) bool {
				if b, ok := n.(*ast.BlockStmt); ok && n != stmt {
					walkBlocks(b.List)
					return false
				}
				return !okProven
			})
		}
	}
	walkBlocks(body.List)
	return okProven
}
