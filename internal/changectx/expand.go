package changectx

import (
	"fmt"
	"go/token"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// historyRevisions caps how far back the history role reads per line span.
// A handful of revisions is enough to show that a line was deliberate, and it
// keeps a file rewritten fifty times from burying the rest of the envelope.
const historyRevisions = 3

// historyRangesPerFile caps how many spans of one file get their own history.
const historyRangesPerFile = 3

// callerContextLines is how much surrounding code a call site carries. A call
// alone does not say what it is guarding or what it does with the result.
const callerContextLines = 2

// expand fills the envelope with the code around the change, in the order the
// consumer ranks the roles.
func (b *builder) expand() {
	if len(b.decls) == 0 {
		return
	}
	b.expandEnclosing()
	b.expandUses()
	b.expandTypes()
	b.expandSiblings()
	b.expandHistory()
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
func (b *builder) expandHistory() {
	for _, f := range b.files {
		ranges := b.ranges[f.Path]
		for i, r := range ranges {
			if i >= historyRangesPerFile {
				break
			}
			out, err := logLineHistory(b.repo, f.Path, r, historyRevisions)
			if err != nil || strings.TrimSpace(out) == "" {
				continue
			}
			d := b.declCovering(f.Path, r)
			e := Expansion{
				Role:      RoleHistory,
				File:      f.Path,
				StartLine: r.start,
				EndLine:   r.end,
				Content:   out,
				Details: map[string]string{
					"kind":      "line-history",
					"lines":     fmt.Sprintf("%d-%d", r.start, r.end),
					"revisions": strconv.Itoa(historyRevisions),
				},
			}
			if d != nil {
				e.Priority = priorityFor(d)
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
	ranges, err := removedRanges(b.repo, b.base, path)
	if err != nil {
		return
	}
	for i, r := range ranges {
		if i >= historyRangesPerFile {
			break
		}
		out, err := logRemovedHistory(b.repo, b.base, path, r, historyRevisions)
		if err != nil || strings.TrimSpace(out) == "" {
			continue
		}
		b.add(Expansion{
			Role: RoleHistory,
			// Above the surviving lines' history: why something was removed
			// is a sharper question than why it is still there.
			Priority:  1,
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

// declCovering returns the changed declaration a line span falls in, if the
// span sits inside one.
func (b *builder) declCovering(rel string, r lineRange) *decl {
	for _, d := range b.decls {
		if d.rel == rel && d.overlaps(r) {
			return d
		}
	}
	return nil
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
