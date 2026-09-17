package changectx

import (
	"fmt"
	"go/ast"
	"go/types"
	"sort"
	"strings"
)

// maxCalleesPerDecl caps how many callees one changed declaration contributes.
// A function that calls forty things in this module would otherwise spend the
// consumer's whole budget describing code the change only passes through.
const maxCalleesPerDecl = 12

// expandCallees emits what the change calls: for every changed function, the
// declarations its body reaches that this module declares.
//
// It is the other half of the caller role, and the half no caller can stand in
// for. A change that starts returning nil is judged by the code that reads the
// result, which is a caller. A change to what a handler invalidates, refreshes
// or commits is judged by what that handler calls, and the callers of the
// handler show none of it. Redline's roadmap traces one missed defect of the
// second kind: what a page's refresh actually clears is a callee of its
// handler.
//
// Only calls this module declares are emitted. A call into the standard
// library or a dependency resolves to a position outside the work tree, and
// the envelope refuses to name a path the reviewer is not on.
//
// A callee already in the change is skipped. Its declaration is the change,
// and the enclosing role carries it. So is a callee in a test file: the test
// role carries tests, and a changed test would otherwise drag its own
// scaffolding in under a role meant for contracts.
func (b *builder) expandCallees() {
	type group struct {
		target   *decl
		from     []string
		priority int
	}
	var order []*group
	byDecl := map[*decl]*group{}
	for _, d := range b.decls {
		if d.fn == nil || d.fn.Body == nil {
			continue
		}
		info := b.typesInfoFor(d)
		if info == nil {
			continue
		}
		kept, dropped := 0, 0
		for _, obj := range calleeObjects(d.fn.Body, info) {
			target := b.declFor(obj)
			if target == nil || target == d || b.isChanged(target) {
				continue
			}
			// Never into a test file. A test helper is not the other half of a
			// contract, and the test role already carries tests; without this a
			// changed test drags its own scaffolding in as context.
			if strings.HasSuffix(target.rel, "_test.go") {
				continue
			}
			if _, seen := byDecl[target]; !seen {
				if kept >= maxCalleesPerDecl {
					dropped++
					continue
				}
				kept++
			}
			g, ok := byDecl[target]
			if !ok {
				g = &group{target: target}
				byDecl[target] = g
				order = append(order, g)
			}
			g.from = append(g.from, d.scope)
			if p := priorityFor(target); p > g.priority {
				g.priority = p
			}
		}
		if dropped > 0 {
			b.notes = append(b.notes, fmt.Sprintf(
				"%d further callee(s) of %s were not expanded (cap %d per declaration)",
				dropped, d.scope, maxCalleesPerDecl))
		}
	}
	for _, g := range order {
		details := g.target.details()
		details["calledBy"] = strings.Join(sortedUnique(g.from), ", ")
		b.add(Expansion{
			Role:      RoleCallee,
			Priority:  g.priority,
			Symbol:    g.target.symbol,
			Scope:     g.target.scope,
			File:      g.target.rel,
			StartLine: g.target.start,
			EndLine:   g.target.end,
			Content:   b.slice(g.target.rel, g.target.start, g.target.end),
			Details:   details,
		})
	}
}

// typesInfoFor returns the type information for the package a declaration was
// loaded from.
func (b *builder) typesInfoFor(d *decl) *types.Info {
	if d == nil || d.unit == nil || d.unit.pkg == nil {
		return nil
	}
	return d.unit.pkg.TypesInfo
}

// calleeObjects returns the functions a body calls, in source order, resolved
// through the type checker rather than by name.
//
// The order is the source order of the calls so the output does not depend on
// map iteration, which an envelope that has to compare with itself cannot
// afford.
func calleeObjects(body *ast.BlockStmt, info *types.Info) []types.Object {
	type hit struct {
		pos int
		obj types.Object
	}
	var hits []hit
	seen := map[types.Object]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		id := calleeIdent(call.Fun)
		if id == nil {
			return true
		}
		obj := info.Uses[id]
		fn, ok := obj.(*types.Func)
		if !ok || seen[fn] {
			return true
		}
		seen[fn] = true
		hits = append(hits, hit{pos: int(id.Pos()), obj: fn})
		return true
	})
	sort.Slice(hits, func(i, j int) bool { return hits[i].pos < hits[j].pos })
	out := make([]types.Object, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.obj)
	}
	return out
}
