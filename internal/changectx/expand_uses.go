package changectx

import (
	"fmt"
	"go/ast"
	"go/token"
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
	var tests, callers []useSite
	for _, s := range sites {
		if strings.HasSuffix(s.rel, "_test.go") {
			tests = append(tests, s)
			continue
		}
		callers = append(callers, s)
	}
	b.addIndirectCallerSites(b.addCallerSites(callers))
	b.addTestSites(tests)
}

// addCallerSites emits the whole calling function, once per function, naming
// every changed symbol it reaches.
//
// What it used to send was the use line and two either side. That window
// cannot show a nil check five lines up, what the caller does with a returned
// value, or what a handler clears before it returns, and those are the
// relationships a contract defect turns on. The enclosing declaration carries
// them, and it was already resolved right here to fill in details.callerSymbol.
//
// Keyed on the declaration for the reason the test role beside it is: one
// function that reaches three changed symbols is one expansion naming three,
// not three copies of one body. Keying on the line was enough while an
// expansion was five lines wide and is not now — two uses in one function,
// twenty lines apart, would ship that function twice.
func (b *builder) addCallerSites(sites []useSite) []*decl {
	type group struct {
		encl     *decl
		calls    []string
		kinds    []string
		lines    []string
		uses     []string
		priority int
	}
	var order []*group
	byDecl := map[*decl]*group{}
	for _, s := range sites {
		encl := b.enclosingAt(s.rel, s.line)
		if encl == nil {
			// A use with no declaration around it: an import alias, or a file
			// whose declarations did not resolve. The window is all there is.
			b.addCallerWindow(s)
			continue
		}
		g, ok := byDecl[encl]
		if !ok {
			g = &group{encl: encl}
			byDecl[encl] = g
			order = append(order, g)
		}
		g.calls = append(g.calls, s.target.scope)
		g.kinds = append(g.kinds, s.kind)
		g.lines = append(g.lines, strconv.Itoa(s.line))
		g.uses = append(g.uses, strconv.Itoa(s.line)+":"+s.kind)
		if p := priorityFor(s.target); p > g.priority {
			g.priority = p
		}
	}
	for _, g := range order {
		details := map[string]string{
			"kind":         strings.Join(sortedUnique(g.kinds), ", "),
			"calls":        strings.Join(sortedUnique(g.calls), ", "),
			"line":         g.lines[0],
			"callerSymbol": g.encl.scope,
			"callerKind":   g.encl.kind,
		}
		// Where the uses sit and what each one is, so a reader of a long
		// function is not left to find them, a consumer can still preview the
		// use in an index, and a function holding both a call and a bare
		// reference does not lose which line is which to the joined kind.
		if len(g.uses) > 1 {
			details["uses"] = strings.Join(sortedUnique(g.uses), ", ")
		}
		b.add(Expansion{
			Role:      RoleCaller,
			Priority:  g.priority,
			Symbol:    g.encl.symbol,
			Scope:     g.encl.scope,
			File:      g.encl.rel,
			StartLine: g.encl.start,
			EndLine:   g.encl.end,
			Content:   b.slice(g.encl.rel, g.encl.start, g.encl.end),
			Details:   details,
		})
	}
	out := make([]*decl, 0, len(order))
	for _, g := range order {
		out = append(out, g.encl)
	}
	return out
}

// addCallerWindow is the fallback for a use no declaration encloses.
func (b *builder) addCallerWindow(s useSite) {
	start := max(s.line-callerContextLines, 1)
	end := s.line + callerContextLines
	b.add(Expansion{
		Role:      RoleCaller,
		Priority:  priorityFor(s.target),
		Symbol:    s.target.symbol,
		Scope:     s.target.scope,
		File:      s.rel,
		StartLine: start,
		EndLine:   end,
		Content:   b.slice(s.rel, start, end),
		Details: map[string]string{
			"kind": s.kind, "line": strconv.Itoa(s.line), "span": "window",
		},
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
	// Keyed by the declaration itself, which declsForRel hands out once per
	// file, rather than by its name: a name is not an identity, and a file
	// may declare several functions called init.
	byDecl := map[*decl]*group{}
	for _, s := range sites {
		encl := b.enclosingAt(s.rel, s.line)
		if encl == nil {
			continue
		}
		g, ok := byDecl[encl]
		if !ok {
			g = &group{encl: encl}
			byDecl[encl] = g
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
	return b.collectUsesOf(b.changedByPos(), b.insideChanged)
}

// collectUsesOf is the walk behind both hops: it finds every identifier the
// type checker resolved to one of the declarations in byPos, skipping uses
// that sit inside code the caller of this already accounts for.
func (b *builder) collectUsesOf(byPos map[token.Pos]*decl, skip func(rel string, line int) bool) []useSite {
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
			if skip != nil && skip(rel, pos.Line) {
				continue
			}
			// Keyed on the line, not the column: a caller carries the lines
			// around the use, so two uses of one symbol on one line —
			// Half(Half(n)) — shipped the same expansion twice.
			key := rel + ":" + strconv.Itoa(pos.Line) + ":" + target.scope
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
