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

// calleeGroup is one emitted callee and the changed declarations reaching it.
type calleeGroup struct {
	target   *decl
	from     []string
	priority int
}

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
	var order []*calleeGroup
	byDecl := map[*decl]*calleeGroup{}
	record := func(target, from *decl) {
		g, ok := byDecl[target]
		if !ok {
			g = &calleeGroup{target: target}
			byDecl[target] = g
			order = append(order, g)
		}
		g.from = append(g.from, from.scope)
		if p := priorityFor(target); p > g.priority {
			g.priority = p
		}
	}
	for _, d := range b.decls {
		if dropped := b.collectCallees(d, byDecl, record); dropped > 0 {
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

// collectCallees records what one changed declaration calls, and returns how
// many were dropped to the cap. A declaration already carried by the groups
// does not count against this one's cap: it costs nothing more to name.
func (b *builder) collectCallees(d *decl, byDecl map[*decl]*calleeGroup, record func(target, from *decl)) int {
	info := b.typesInfoFor(d)
	if d.fn == nil || d.fn.Body == nil || info == nil {
		return 0
	}
	kept, dropped := 0, 0
	for _, obj := range calleeObjects(d.fn.Body, info) {
		target := b.declFor(obj)
		if !b.calleeWorthEmitting(d, target) {
			continue
		}
		if _, seen := byDecl[target]; !seen {
			if kept >= maxCalleesPerDecl {
				dropped++
				continue
			}
			kept++
		}
		record(target, d)
	}
	return dropped
}

// calleeWorthEmitting reports whether a resolved callee belongs in the role.
//
// Never the declaration doing the calling, never one the change already
// carries -- the enclosing role has that -- and never one in a test file: a
// test helper is not the other half of a contract, and without this a changed
// test drags its own scaffolding in as context.
func (b *builder) calleeWorthEmitting(from, target *decl) bool {
	if target == nil || target == from || b.isChanged(target) {
		return false
	}
	return !strings.HasSuffix(target.rel, "_test.go")
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
