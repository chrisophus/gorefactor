package changectx

import (
	"fmt"
	"go/token"
	"strconv"
	"strings"
)

// maxIndirectCallers caps the second hop across the whole change, not per
// declaration. The first hop is bounded by how many places reach the change;
// the second is bounded by how many places reach those, which on a widely used
// helper is most of the module. This is the least valuable role here, so it is
// the one with a hard ceiling.
const maxIndirectCallers = 8

// addIndirectCallerSites emits the second hop: the declarations that call the
// direct callers of a changed symbol.
//
// It exists because one hop is often not where the caller's own contract is
// decided. A changed function returns a new error, its caller passes the error
// up, and whether that matters is decided in the caller's caller, which handles
// it or drops it. The first hop shows the pass-through and answers nothing.
//
// It is ranked last by the consumer, below history, and capped hard, because it
// is whole declarations that may have nothing to do with the change. A deferred
// index can carry it at one line each and the reviewer can decide; a prompt
// that must fit cannot.
func (b *builder) addIndirectCallerSites(direct []*decl) {
	if len(direct) == 0 {
		return
	}
	byPos := map[token.Pos]*decl{}
	for _, d := range direct {
		// Only changed declarations are resolved when the change is read, and
		// these are the callers of those. Resolving is idempotent and skips a
		// declaration whose package did not type-check, which is what keeps a
		// second hop from being guessed at by name.
		if d.obj == nil {
			resolveObject(d)
		}
		if d.obj != nil {
			byPos[d.obj.Pos()] = d
		}
	}
	if len(byPos) == 0 {
		return
	}
	// Skip anything already carried by a nearer role: the change itself, and
	// the direct callers, whose bodies the caller role has just emitted whole.
	inDirect := func(rel string, line int) bool {
		for _, d := range direct {
			if d.rel == rel && d.start <= line && line <= d.end {
				return true
			}
		}
		return false
	}
	sites := b.collectUsesOf(byPos, func(rel string, line int) bool {
		return b.insideChanged(rel, line) || inDirect(rel, line)
	})

	type group struct {
		encl     *decl
		reaches  []string
		lines    []string
		priority int
	}
	var order []*group
	byDecl := map[*decl]*group{}
	dropped := 0
	for _, s := range sites {
		// A test that reaches a caller is not a second hop worth paying for:
		// the test role already carries the tests that reach the change.
		if strings.HasSuffix(s.rel, "_test.go") {
			continue
		}
		encl := b.enclosingAt(s.rel, s.line)
		if encl == nil || b.isChanged(encl) {
			continue
		}
		g, ok := byDecl[encl]
		if !ok {
			if len(order) >= maxIndirectCallers {
				dropped++
				continue
			}
			g = &group{encl: encl}
			byDecl[encl] = g
			order = append(order, g)
		}
		g.reaches = append(g.reaches, s.target.scope)
		g.lines = append(g.lines, strconv.Itoa(s.line))
		if p := priorityFor(s.target); p > g.priority {
			g.priority = p
		}
	}
	if dropped > 0 {
		b.notes = append(b.notes, fmt.Sprintf(
			"%d further indirect caller(s) were not expanded (cap %d)", dropped, maxIndirectCallers))
	}
	for _, g := range order {
		b.add(Expansion{
			Role:      RoleIndirectCaller,
			Priority:  g.priority,
			Symbol:    g.encl.symbol,
			Scope:     g.encl.scope,
			File:      g.encl.rel,
			StartLine: g.encl.start,
			EndLine:   g.encl.end,
			Content:   b.slice(g.encl.rel, g.encl.start, g.encl.end),
			Details: map[string]string{
				"kind": g.encl.kind,
				"hop":  "2",
				// The direct callers this one reaches, so the path from the
				// change is readable without opening both expansions.
				"reaches": strings.Join(sortedUnique(g.reaches), ", "),
				"line":    g.lines[0],
			},
		})
	}
}
