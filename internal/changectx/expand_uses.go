package changectx

import (
	"fmt"
	"go/ast"
	"go/types"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"
)

// useSite is one place the code reads a changed symbol, found through the type
// checker. Name matching would also find the string "Insert" in an unrelated
// package, and a reviewer who is shown one wrong call site stops trusting all
// of them.
type useSite struct {
	rel    string
	line   int
	col    int
	kind   string // call-site or reference-site, from what the identifier does
	target *decl
}

// sortKey orders a use site by where it sits. The line and column are padded
// so the string compares in numeric order.
func (u useSite) sortKey() string {
	return fmt.Sprintf("%s:%08d:%08d:%s", u.rel, u.line, u.col, u.target.scope)
}

// expandUses emits the caller and test roles. Both come from the same walk:
// every identifier the type checker resolved to a changed declaration.
//
// Call sites of unexported declarations count. Gating them out was the first
// shape of this and it was wrong twice over. It dropped exactly the case the
// role exists for, since most signature changes are to unexported functions
// used within their own package, and it was inconsistent with the test role
// beside it, which never had the gate: a reviewer was shown the test calling
// a changed unexported function and not the production code calling it.
// Exportedness is a ranking input rather than a filter, and priorityFor
// already scores it, so the consumer's budget drops these first when space is
// short instead of never seeing them.
func (b *builder) expandUses() {
	sites := b.collectUses()
	var tests []useSite
	for _, s := range sites {
		if strings.HasSuffix(s.rel, "_test.go") {
			tests = append(tests, s)
			continue
		}
		b.addCallerSite(s)
	}
	b.addTestSites(tests)
}

func (b *builder) addCallerSite(s useSite) {
	start := max(s.line-callerContextLines, 1)
	end := s.line + callerContextLines
	details := map[string]string{"kind": s.kind, "line": strconv.Itoa(s.line)}
	if encl := b.enclosingAt(s.rel, s.line); encl != nil {
		details["callerSymbol"] = encl.scope
		details["callerKind"] = encl.kind
	}
	b.add(Expansion{
		Role:      RoleCaller,
		Priority:  priorityFor(s.target),
		Symbol:    s.target.symbol,
		Scope:     s.target.scope,
		File:      s.rel,
		StartLine: start,
		EndLine:   end,
		Content:   b.slice(s.rel, start, end),
		Details:   details,
	})
}

// addTestSites emits the whole test function that reaches a changed symbol,
// once, naming every changed symbol it reaches. The assertions are the part
// that says what the symbol is supposed to do, and they are usually below the
// call.
//
// The key is the function, not the function and the symbol. Keying on both
// shipped one test once per symbol it touched: on this provider's own first
// change one table test travelled thirteen times, byte for byte, differing
// only in details.covers, and three quarters of the test role's bytes were
// duplicate copies. One expansion listing every symbol it covers says more
// than thirteen each naming one.
func (b *builder) addTestSites(sites []useSite) {
	type group struct {
		encl     *decl
		covers   []string
		symbols  []string
		priority int
	}
	var order []*group
	byKey := map[string]*group{}
	for _, s := range sites {
		encl := b.enclosingAt(s.rel, s.line)
		if encl == nil {
			continue
		}
		key := encl.rel + ":" + encl.symbol
		g, ok := byKey[key]
		if !ok {
			g = &group{encl: encl}
			byKey[key] = g
			order = append(order, g)
		}
		g.covers = append(g.covers, s.target.scope)
		g.symbols = append(g.symbols, s.target.symbol)
		if p := priorityFor(s.target); p > g.priority {
			g.priority = p
		}
	}
	for _, g := range order {
		b.add(Expansion{
			Role:      RoleTest,
			Priority:  g.priority,
			Symbol:    g.encl.symbol,
			Scope:     g.encl.scope,
			File:      g.encl.rel,
			StartLine: g.encl.start,
			EndLine:   g.encl.end,
			Content:   b.slice(g.encl.rel, g.encl.start, g.encl.end),
			Details: map[string]string{
				"kind":    "test",
				"covers":  strings.Join(sortedUnique(g.covers), ", "),
				"testFor": strings.Join(sortedUnique(g.symbols), ", "),
			},
		})
	}
}

// collectUses walks the type information of every loaded package variant and
// keeps the identifiers that resolve to a changed declaration. The result is
// sorted because the walk is over maps, and an envelope whose order depends on
// map iteration cannot be compared with itself.
func (b *builder) collectUses() []useSite {
	byPos := b.changedByPos()
	if len(byPos) == 0 || b.idx == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []useSite
	for _, p := range b.idx.pkgs {
		if p.TypesInfo == nil || p.Fset == nil {
			continue
		}
		calls := callFuns(p)
		for id, obj := range p.TypesInfo.Uses {
			if obj == nil {
				continue
			}
			target := byPos[obj.Pos()]
			if target == nil {
				continue
			}
			pos := p.Fset.Position(id.Pos())
			rel, ok := b.rel(pos.Filename)
			if !ok {
				continue
			}
			if b.insideChanged(rel, pos.Line) {
				continue
			}
			key := rel + ":" + strconv.Itoa(pos.Line) + ":" + strconv.Itoa(pos.Column) + ":" + target.scope
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, useSite{
				rel: rel, line: pos.Line, col: pos.Column,
				kind: useKind(obj, calls[id]), target: target,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].sortKey() < out[j].sortKey() })
	return out
}

// insideChanged reports whether a line falls inside a declaration the diff
// touched.
//
// These roles answer what else the change affects, and code inside the change
// is not that: the enclosing role already ships those lines whole. Skipping
// only the use's own declaration kept every use that sat in a different
// changed one, and on this provider's own first change 46 of 64 caller
// expansions fell entirely inside a span the enclosing role had already
// emitted. A self-contained new package leaves the role empty, which is the
// honest answer.
func (b *builder) insideChanged(rel string, line int) bool {
	for _, d := range b.decls {
		if d.rel == rel && d.start <= line && line <= d.end {
			return true
		}
	}
	return false
}

// callFuns collects the identifiers a call calls: the function or method
// position of every call in a package.
func callFuns(p *packages.Package) map[*ast.Ident]bool {
	out := map[*ast.Ident]bool{}
	for _, file := range p.Syntax {
		ast.Inspect(file, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				if id := calleeIdent(call.Fun); id != nil {
					out[id] = true
				}
			}
			return true
		})
	}
	return out
}

// calleeIdent unwraps what a call calls down to the identifier naming it,
// through a selector, a generic instantiation and the parentheses a callee is
// occasionally written with.
func calleeIdent(e ast.Expr) *ast.Ident {
	switch f := e.(type) {
	case *ast.Ident:
		return f
	case *ast.SelectorExpr:
		return f.Sel
	case *ast.ParenExpr:
		return calleeIdent(f.X)
	case *ast.IndexExpr:
		return calleeIdent(f.X)
	case *ast.IndexListExpr:
		return calleeIdent(f.X)
	}
	return nil
}

// useKind labels what a use does with the symbol it reaches. The walk behind
// this role is over every resolved identifier, so a type reference, a field
// type and a const initialiser all arrive beside the calls; labelling them
// all "call-site" put a claim on 27 of 64 expansions that the code they
// carried plainly contradicted, and a reviewer shown one wrong call site
// stops trusting all of them. A conversion is a call in the syntax and not in
// the sense that matters here, so a type is never a call site.
func useKind(obj types.Object, called bool) string {
	if _, isType := obj.(*types.TypeName); called && !isType {
		return "call-site"
	}
	return "reference-site"
}
