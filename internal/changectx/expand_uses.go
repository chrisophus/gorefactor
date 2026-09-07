package changectx

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// useSite is one place the code reads a changed symbol, found through the type
// checker. Name matching would also find the string "Insert" in an unrelated
// package, and a reviewer who is shown one wrong call site stops trusting all
// of them.
type useSite struct {
	rel    string
	line   int
	col    int
	target *decl
}

// sortKey orders a use site by where it sits. The line and column are padded
// so the string compares in numeric order.
func (u useSite) sortKey() string {
	return fmt.Sprintf("%s:%08d:%08d:%s", u.rel, u.line, u.col, u.target.scope)
}

// expandUses emits the caller and test roles. Both come from the same walk:
// every identifier the type checker resolved to a changed declaration.
func (b *builder) expandUses() {
	sites := b.collectUses()
	seenTest := map[string]bool{}
	for _, s := range sites {
		if strings.HasSuffix(s.rel, "_test.go") {
			b.addTestSite(s, seenTest)
			continue
		}
		if !s.target.exported {
			continue
		}
		b.addCallerSite(s)
	}
}

func (b *builder) addCallerSite(s useSite) {
	start := max(s.line-callerContextLines, 1)
	end := s.line + callerContextLines
	details := map[string]string{"kind": "call-site", "line": strconv.Itoa(s.line)}
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

// addTestSite emits the whole test function that reaches a changed symbol. The
// assertions are the part that says what the symbol is supposed to do, and
// they are usually below the call.
func (b *builder) addTestSite(s useSite, seen map[string]bool) {
	encl := b.enclosingAt(s.rel, s.line)
	if encl == nil {
		return
	}
	key := encl.rel + ":" + encl.symbol + ":" + s.target.scope
	if seen[key] {
		return
	}
	seen[key] = true
	b.add(Expansion{
		Role:      RoleTest,
		Priority:  priorityFor(s.target),
		Symbol:    encl.symbol,
		Scope:     encl.scope,
		File:      encl.rel,
		StartLine: encl.start,
		EndLine:   encl.end,
		Content:   b.slice(encl.rel, encl.start, encl.end),
		Details: map[string]string{
			"kind":    "test",
			"covers":  s.target.scope,
			"testFor": s.target.symbol,
		},
	})
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
			if rel == target.rel && pos.Line >= target.start && pos.Line <= target.end {
				continue // the declaration itself, or a call it makes to itself
			}
			key := rel + ":" + strconv.Itoa(pos.Line) + ":" + strconv.Itoa(pos.Column) + ":" + target.scope
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, useSite{rel: rel, line: pos.Line, col: pos.Column, target: target})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].sortKey() < out[j].sortKey() })
	return out
}
