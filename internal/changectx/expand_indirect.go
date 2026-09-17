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

// hopGroup is one declaration at the second hop and the direct callers it
// reaches the change through.
type hopGroup struct {
	encl     *decl
	reaches  []string
	lines    []string
	priority int
}

// resolvedByPos keys declarations by their type-checker object position.
//
// Only changed declarations are resolved when the change is read, and these are
// the callers of those, so without this the second hop finds nothing at all --
// which is what the first run of it did. Resolving is idempotent and skips a
// declaration whose package did not type-check, which is what keeps a hop from
// being guessed at by name.
func resolvedByPos(decls []*decl) map[token.Pos]*decl {
	out := map[token.Pos]*decl{}
	for _, d := range decls {
		if d.obj == nil {
			resolveObject(d)
		}
		if d.obj != nil {
			out[d.obj.Pos()] = d
		}
	}
	return out
}

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
	byPos := resolvedByPos(direct)
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

	order, indirectTests, dropped := b.groupHops(sites)
	if dropped > 0 {
		b.notes = append(b.notes, fmt.Sprintf(
			"%d further indirect caller(s) were not expanded (cap %d)", dropped, maxIndirectCallers))
	}
	b.addIndirectTestSites(indirectTests)
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

// groupHops sorts the second-hop uses into the declarations that hold them,
// setting the tests aside for the test role, and reports how many declarations
// the cap turned away.
//
// A test at the second hop is a test, not a caller. The test role carries the
// tests that name a changed symbol; one that reaches it through another
// declaration -- usually from another package -- is the case that role never
// covered, and it is still the answer to "what checks this".
func (b *builder) groupHops(sites []useSite) (order []*hopGroup, tests []useSite, dropped int) {
	byDecl := map[*decl]*hopGroup{}
	for _, s := range sites {
		if strings.HasSuffix(s.rel, "_test.go") {
			tests = append(tests, s)
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
			g = &hopGroup{encl: encl}
			byDecl[encl] = g
			order = append(order, g)
		}
		g.reaches = append(g.reaches, s.target.scope)
		g.lines = append(g.lines, strconv.Itoa(s.line))
		if p := priorityFor(s.target); p > g.priority {
			g.priority = p
		}
	}
	return order, tests, dropped
}

// addIndirectTestSites emits the tests that reach the change through one of its
// callers, under the test role.
//
// The test role finds tests that name a changed symbol. A test in another
// package usually does not: it exercises the function that calls the change,
// which is the test most likely to fail and the one nothing here reported.
// details.hop says it is not a direct test, so a reviewer reading "what checks
// this" knows how far away the check sits.
func (b *builder) addIndirectTestSites(sites []useSite) {
	var order []*hopGroup
	byDecl := map[*decl]*hopGroup{}
	for _, s := range sites {
		encl := b.enclosingAt(s.rel, s.line)
		if encl == nil || b.isChanged(encl) {
			continue
		}
		g, ok := byDecl[encl]
		if !ok {
			if len(order) >= maxIndirectCallers {
				continue
			}
			g = &hopGroup{encl: encl}
			byDecl[encl] = g
			order = append(order, g)
		}
		g.reaches = append(g.reaches, s.target.scope)
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
				"hop":     "2",
				"reaches": strings.Join(sortedUnique(g.reaches), ", "),
			},
		})
	}
}
