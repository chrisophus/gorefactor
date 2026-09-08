package changectx

import (
	"fmt"
	"go/token"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// historyRevisions caps how far back the history role reads per line span.
// A handful of revisions is enough to show that a line was deliberate, and it
// keeps a file rewritten fifty times from burying the rest of the envelope.
const historyRevisions = 3

// historyRangesPerFile caps how many spans of one file get their own history.
const historyRangesPerFile = 3

// removedHistoryPriority ranks a deleted span's history above the surviving
// lines' history: why something was removed is a sharper question than why it
// is still there. priorityFor scores a declaration in the 50..100 band, so
// this has to sit above that band to outrank it: the consumer sorts
// descending within a role and drops the tail when the budget binds.
const removedHistoryPriority = 120

// callerContextLines is how much surrounding code a call site carries. A call
// alone does not say what it is guarding or what it does with the result.
const callerContextLines = 2

// expand fills the envelope with the code around the change, in the order the
// consumer ranks the roles.
//
// The roles that read declarations need declarations; history does not, and
// is driven from the manifest instead. A change that only deletes files
// resolves to no declaration at all, and that is precisely where history
// earns its place: the lines are gone, so nothing else in the envelope says
// why they were there.
func (b *builder) expand() {
	if len(b.decls) > 0 {
		b.expandEnclosing()
		b.expandUses()
		b.expandTypes()
		b.expandSiblings()
	}
	b.expandHistory()
	b.noteEmptyRoles()
}

// expandEnclosing emits the whole declaration each changed hunk sits inside.
// A hunk without it cannot be judged at all, which is why this role is first.
func (b *builder) expandEnclosing() {
	for _, d := range b.decls {
		b.add(Expansion{
			Role:      RoleEnclosing,
			Priority:  priorityFor(d),
			Symbol:    d.symbol,
			Scope:     d.scope,
			File:      d.rel,
			StartLine: d.start,
			EndLine:   d.end,
			Content:   b.slice(d.rel, d.start, d.end),
			Details:   d.details(),
		})
	}
}

// expandHistory emits the recent history of the changed line spans. It is
// cheap, and it stops a whole class of bad review comment: the suggestion to
// undo a deliberate fix.
//
// The span is asked for in base coordinates and reported in working-tree
// ones. Walking base with a working-tree position traces whatever happens to
// sit at that offset in the older file, which on any file whose earlier hunks
// shifted line numbers is not the code under review — and git reports no
// error for it.
func (b *builder) expandHistory() {
	for _, f := range b.files {
		sides, err := hunkSides(b.repo, b.base, f.Path)
		if err != nil {
			b.notes = append(b.notes, "no history for "+f.Path+": "+err.Error())
			continue
		}
		sides, dropped := rankedSides(sides, historyRangesPerFile)
		if dropped > 0 {
			b.notes = append(b.notes, fmt.Sprintf(
				"%d further changed span(s) of %s were not traced (cap %d per file)",
				dropped, f.Path, historyRangesPerFile))
		}
		for _, s := range sides {
			out, err := logLineHistory(b.repo, b.base, f.Path, s.base, historyRevisions)
			if err != nil || strings.TrimSpace(out) == "" {
				continue
			}
			d, priority := b.historyContext(f.Path, s.head)
			e := Expansion{
				Role:      RoleHistory,
				Priority:  priority,
				File:      f.Path,
				StartLine: s.head.start,
				EndLine:   s.head.end,
				Content:   out,
				Details: map[string]string{
					"kind":      "line-history",
					"lines":     fmt.Sprintf("%d-%d", s.head.start, s.head.end),
					"baseLines": fmt.Sprintf("%d-%d", s.base.start, s.base.end),
					"revisions": strconv.Itoa(historyRevisions),
				},
			}
			if d != nil {
				e.Symbol = d.symbol
				e.Scope = d.scope
			}
			b.add(e)
		}
		b.expandRemovedHistory(f.Path)
	}
}

// expandRemovedHistory traces the lines this change deletes.
//
// A deletion is where history earns its place. The lines are gone, so nothing
// in the diff says why they were there, and a guard added on purpose reads
// exactly like a tidy simplification. Tracing the span against the base
// revision recovers the commit that introduced it, and with it the reason.
func (b *builder) expandRemovedHistory(path string) {
	all, err := removedRanges(b.repo, b.base, path)
	if err != nil {
		return
	}
	ranges, dropped := rankedRanges(all, historyRangesPerFile)
	if dropped > 0 {
		b.notes = append(b.notes, fmt.Sprintf(
			"%d further removed span(s) of %s were not traced (cap %d per file)",
			dropped, path, historyRangesPerFile))
	}
	for _, r := range ranges {
		out, err := logRemovedHistory(b.repo, b.base, path, r, historyRevisions)
		if err != nil || strings.TrimSpace(out) == "" {
			continue
		}
		b.add(Expansion{
			Role:      RoleHistory,
			Priority:  removedHistoryPriority,
			File:      path,
			StartLine: r.start,
			EndLine:   r.end,
			Content:   out,
			Details: map[string]string{
				"kind":      "removed-line-history",
				"lines":     fmt.Sprintf("%d-%d at the base revision", r.start, r.end),
				"revisions": strconv.Itoa(historyRevisions),
			},
		})
	}
}

// rankedRanges applies the per-file cap to a set of changed spans, keeping
// the longest ones. The spans arrive sorted by position, so keeping the first
// few keeps whatever sits nearest the top of the file: on this provider's own
// first change that meant a one-line map edit and a three-line metadata edit
// survived while the span holding the added logic was dropped. Span length is
// the proxy for substance, being how many lines the diff touched there.
//
// The second return is how many spans were dropped. The caller reports it as
// a note, because a file whose history was cut has to say so; otherwise the
// envelope reads as a file whose remaining spans were all there was.
func rankedRanges(ranges []lineRange, limit int) ([]lineRange, int) {
	if len(ranges) <= limit {
		return ranges, 0
	}
	ranked := append([]lineRange(nil), ranges...)
	sort.Slice(ranked, func(i, j int) bool {
		li, lj := ranked[i].end-ranked[i].start, ranked[j].end-ranked[j].start
		if li != lj {
			return li > lj
		}
		return ranked[i].start < ranked[j].start
	})
	kept := ranked[:limit]
	sort.Slice(kept, func(i, j int) bool { return kept[i].start < kept[j].start })
	return kept, len(ranges) - limit
}

// rankedSides applies the same cap to paired hunk halves, ranking on the
// working-tree span because that is the side whose size says how much of the
// change the span accounts for.
func rankedSides(sides []hunkSide, limit int) ([]hunkSide, int) {
	if len(sides) <= limit {
		return sides, 0
	}
	ranked := append([]hunkSide(nil), sides...)
	sort.Slice(ranked, func(i, j int) bool {
		li := ranked[i].head.end - ranked[i].head.start
		lj := ranked[j].head.end - ranked[j].head.start
		if li != lj {
			return li > lj
		}
		return ranked[i].head.start < ranked[j].head.start
	})
	kept := ranked[:limit]
	sort.Slice(kept, func(i, j int) bool { return kept[i].head.start < kept[j].head.start })
	return kept, len(sides) - limit
}

// historyContext returns the changed declaration a history span belongs to,
// and the priority the expansion carries.
//
// A span covering more than one changed declaration belongs to none of them.
// The whole-file span of an added file covers every declaration in it, and
// since the declarations are ordered by position, naming one meant naming
// whichever happened to be declared at the top of the file: a 315-line
// expansion was labelled with the first option struct it contained and ranked
// by that struct's own change. Such a span is left unlabelled and scored from
// everything the change touched inside it.
func (b *builder) historyContext(rel string, r lineRange) (*decl, int) {
	var covered []*decl
	for _, d := range b.decls {
		if d.rel == rel && d.overlaps(r) {
			covered = append(covered, d)
		}
	}
	switch len(covered) {
	case 0:
		return nil, 0
	case 1:
		return covered[0], priorityFor(covered[0])
	}
	// Scored through priorityFor so a whole-file span lands on the same scale
	// as a span inside one declaration.
	whole := &decl{}
	for _, d := range covered {
		whole.changed += d.changed
		whole.exported = whole.exported || d.exported
	}
	return nil, priorityFor(whole)
}

// noteEmptyRoles records why a role produced nothing. A role that is merely
// absent is indistinguishable from a stage that crashed, and nothing
// downstream can tell the two apart: on this provider's own first change the
// type and sibling roles were both legitimately empty and the notes were
// empty too. Each note states the condition that emptied the role, which is
// knowable here and nowhere else.
func (b *builder) noteEmptyRoles() {
	present := map[Role]bool{}
	for _, e := range b.exps {
		present[e.Role] = true
	}
	for _, role := range []Role{RoleEnclosing, RoleCaller, RoleType, RoleSibling, RoleTest, RoleHistory} {
		if !present[role] {
			b.notes = append(b.notes, "no "+string(role)+" expansions: "+b.emptyRoleReason(role))
		}
	}
}

// emptyRoleReason names what left a role empty. Every role but history reads
// declarations, so an unresolved change explains all of them at once.
func (b *builder) emptyRoleReason(role Role) string {
	if len(b.decls) == 0 && role != RoleHistory {
		return "the change resolved to no Go declaration"
	}
	switch role {
	case RoleEnclosing:
		return "the changed declarations had no readable content in the working tree"
	case RoleCaller:
		return "nothing outside the change references a changed symbol"
	case RoleTest:
		return "no test outside the change reaches a changed symbol"
	case RoleType:
		return "the changed signatures name no type declared outside the change, which is the case for a new package: its own types are part of the change"
	case RoleSibling:
		return "no changed type implements an interface declared in this module"
	case RoleHistory:
		return "git reported no history for the changed spans"
	}
	return "the stage produced nothing"
}

// rel converts an absolute path to the repo-relative, forward-slash form the
// envelope carries. A path outside the work tree is refused: an absolute path
// in the output would name a machine the reviewer is not on.
func (b *builder) rel(abs string) (string, bool) {
	r, err := filepath.Rel(b.repo, abs)
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(r), true
}

// declsForRel returns the top-level declarations of any file in the module,
// parsed once and kept. It is how an expansion found by position reports the
// declaration it sits in.
func (b *builder) declsForRel(rel string) []*decl {
	if ds, cached := b.declCache[rel]; cached {
		return ds
	}
	abs := filepath.Join(b.repo, filepath.FromSlash(rel))
	ds := declsIn(b.idx.unit(abs), rel, path.Dir(rel))
	b.declCache[rel] = ds
	return ds
}

// enclosingAt returns the declaration containing a line of a file.
func (b *builder) enclosingAt(rel string, line int) *decl {
	for _, d := range b.declsForRel(rel) {
		if d.start <= line && line <= d.end {
			return d
		}
	}
	return nil
}

// changedByPos indexes the changed declarations by the position of the object
// they declare. Positions are the identity used throughout the expansion
// stages: the loader type-checks a package and its test variant separately, so
// the same declaration yields two objects, and only their shared declaration
// position ties them together.
func (b *builder) changedByPos() map[token.Pos]*decl {
	out := map[token.Pos]*decl{}
	for _, d := range b.decls {
		if d.obj != nil {
			out[d.obj.Pos()] = d
		}
	}
	return out
}
