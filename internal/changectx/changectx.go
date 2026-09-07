// Package changectx builds the context envelope for a change: the manifest of
// changed files, the symbols inside them the diff touched, and the code around
// those symbols that a reviewer needs and the diff does not carry.
//
// The wire format belongs to the consumer, which owns the role vocabulary and
// does the ranking. This package fills the envelope in for Go and stops at
// tagging each expansion with a role and a priority hint.
//
// Expansions leave here whole. The consumer cuts them against its own token
// ceiling, so trimming to a budget here would throw away context it may have
// had room for. That is the opposite of the per-symbol context pack, which
// exists to fit one answer into one prompt.
package changectx

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Options selects the change to describe.
type Options struct {
	// Root is any directory inside the work tree. The envelope reports paths
	// relative to the tree's top level.
	Root string
	// BaseRef is the ref the change is measured against. Its merge base with
	// HEAD becomes the envelope's baseSHA.
	BaseRef string
	// Version is reported as the provider version.
	Version string
}

// builder carries the state of one Build call. Every stage appends to it and
// nothing reads back, which is what keeps the output a function of the
// revision alone.
type builder struct {
	repo      string
	base      string
	idx       *index
	files     []File
	exps      []Expansion
	notes     []string
	decls     []*decl
	ranges    map[string][]lineRange
	lines     map[string][]string
	declCache map[string][]*decl
}

// Build produces the envelope for the change between opts.BaseRef's merge base
// and the working tree.
func Build(opts Options) (*Envelope, error) {
	root := opts.Root
	if root == "" {
		root = "."
	}
	repo, err := repoRoot(root)
	if err != nil {
		return nil, fmt.Errorf("locate the work tree at %s: %w", root, err)
	}
	if resolved, rerr := filepath.EvalSymlinks(repo); rerr == nil {
		repo = resolved
	}
	base, err := mergeBase(repo, opts.BaseRef)
	if err != nil {
		return nil, fmt.Errorf("resolve the merge base with %s: %w", opts.BaseRef, err)
	}
	changes, err := changedFiles(repo, base)
	if err != nil {
		return nil, fmt.Errorf("list the files changed since %s: %w", base, err)
	}

	b := &builder{
		repo:      repo,
		base:      base,
		ranges:    map[string][]lineRange{},
		lines:     map[string][]string{},
		declCache: map[string][]*decl{},
	}
	b.manifest(changes)
	b.resolve(changes)
	b.expand()

	env := &Envelope{
		SchemaVersion:  SchemaVersion,
		Provider:       Provider{Name: providerName, Version: opts.Version, Language: providerLanguage},
		BaseSHA:        base,
		Files:          b.files,
		Expansions:     b.exps,
		PromptFragment: promptFragment,
		Notes:          b.finalNotes(),
	}
	sortExpansions(env.Expansions)
	return env, nil
}

// manifest classifies every changed path.
func (b *builder) manifest(changes []change) {
	for _, c := range changes {
		class, generated := classify(b.repo, c)
		b.files = append(b.files, File{Path: c.path, Class: class, Generated: generated})
	}
}

// reviewable reports whether a changed file is worth resolving symbols in.
// Machine output and vendored code are listed in the manifest and read by
// nobody, so the work of type-checking them buys nothing.
func (b *builder) reviewable(i int) bool {
	f := b.files[i]
	return strings.HasSuffix(f.Path, ".go") && !f.Generated &&
		f.Class != ClassVendored && f.Class != ClassGenerated
}

// resolve maps each changed hunk to the declaration that encloses it.
func (b *builder) resolve(changes []change) {
	byPath := map[string]change{}
	anyGo := false
	for _, c := range changes {
		byPath[c.path] = c
		if strings.HasSuffix(c.path, ".go") {
			anyGo = true
		}
	}
	if !anyGo {
		return
	}
	idx, notes := loadModule(b.repo)
	b.idx = idx
	b.notes = append(b.notes, notes...)

	for i := range b.files {
		f := &b.files[i]
		if !b.reviewable(i) || byPath[f.Path].deleted() {
			continue
		}
		ranges, err := hunkRanges(b.repo, b.base, f.Path)
		if err != nil {
			b.notes = append(b.notes, "no diff hunks for "+f.Path+": "+err.Error())
			continue
		}
		if len(ranges) == 0 {
			ranges = b.wholeFileRange(f.Path)
		}
		b.ranges[f.Path] = ranges
		b.resolveFile(f, ranges)
	}
	sort.Slice(b.decls, func(i, j int) bool { return declLess(b.decls[i], b.decls[j]) })
}

func (b *builder) resolveFile(f *File, ranges []lineRange) {
	abs := filepath.Join(b.repo, filepath.FromSlash(f.Path))
	unit := b.idx.unit(abs)
	if unit == nil || unit.syntax == nil {
		b.notes = append(b.notes, "could not parse "+f.Path+"; its symbols were not resolved")
		return
	}
	for _, d := range b.declsForRel(f.Path) {
		hit := false
		for _, r := range ranges {
			if d.overlaps(r) {
				hit = true
				break
			}
		}
		if !hit {
			continue
		}
		d.changed = d.countChanged(ranges)
		resolveObject(d)
		b.decls = append(b.decls, d)
		f.Symbols = append(f.Symbols, d.symbol)
	}
	f.Symbols = sortedUnique(f.Symbols)
}

// wholeFileRange covers a file the diff reports without hunks, such as one
// that git has not tracked yet.
func (b *builder) wholeFileRange(rel string) []lineRange {
	n := len(b.sourceLines(rel))
	if n == 0 {
		return nil
	}
	return []lineRange{{start: 1, end: n}}
}

// sourceLines reads a working-tree file once and keeps its lines for slicing.
func (b *builder) sourceLines(rel string) []string {
	if lines, ok := b.lines[rel]; ok {
		return lines
	}
	data, err := os.ReadFile(filepath.Join(b.repo, filepath.FromSlash(rel)))
	if err != nil {
		b.lines[rel] = nil
		return nil
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	b.lines[rel] = lines
	return lines
}

// slice returns lines start..end of a file, inclusive and 1-based.
func (b *builder) slice(rel string, start, end int) string {
	lines := b.sourceLines(rel)
	if len(lines) == 0 || start < 1 {
		return ""
	}
	if end > len(lines) {
		end = len(lines)
	}
	if start > end {
		return ""
	}
	return strings.Join(lines[start-1:end], "\n") + "\n"
}

// add records an expansion, dropping ones with no content so an empty string
// never reaches the consumer as if it were context.
func (b *builder) add(e Expansion) {
	if strings.TrimSpace(e.Content) == "" {
		return
	}
	b.exps = append(b.exps, e)
}

// finalNotes sorts and de-duplicates the notes so two runs of the same
// revision report the same unknowns in the same order.
func (b *builder) finalNotes() []string {
	return sortedUnique(b.notes)
}

// priorityFor scores a declaration within its role. Exported symbols outrank
// unexported ones, and a heavily rewritten declaration outranks a one-line
// edit. The scale is local to a role; the consumer never compares across two.
func priorityFor(d *decl) int {
	p := 50
	if d.exported {
		p += 30
	}
	p += min(d.changed, 20)
	return p
}

// details is the Go-shaped half of an expansion. The consumer passes it
// through and renders it generically, which is why nothing language-specific
// belongs in any other field.
func (d *decl) details() map[string]string {
	m := map[string]string{
		"kind":         d.kind,
		"exported":     strconv.FormatBool(d.exported),
		"changedLines": strconv.Itoa(d.changed),
	}
	if d.receiver != "" {
		m["receiver"] = d.receiver
	}
	if d.unit != nil && d.unit.pkg != nil {
		m["package"] = d.unit.pkg.PkgPath
	}
	return m
}

// declLess orders declarations by where they live, so every stage that walks
// them emits in the same order on every run.
func declLess(a, c *decl) bool {
	if a.rel != c.rel {
		return a.rel < c.rel
	}
	if a.start != c.start {
		return a.start < c.start
	}
	return a.symbol < c.symbol
}

// sortExpansions puts the output in the order the consumer will rank it: by
// role, then by the provider's own hint, then by position. The last three keys
// exist only to break ties the same way twice.
func sortExpansions(exps []Expansion) {
	sort.SliceStable(exps, func(i, j int) bool {
		a, c := exps[i], exps[j]
		if ra, rc := roleRank[a.Role], roleRank[c.Role]; ra != rc {
			return ra < rc
		}
		if a.Priority != c.Priority {
			return a.Priority > c.Priority
		}
		if a.File != c.File {
			return a.File < c.File
		}
		if a.StartLine != c.StartLine {
			return a.StartLine < c.StartLine
		}
		if a.Symbol != c.Symbol {
			return a.Symbol < c.Symbol
		}
		return a.Content < c.Content
	})
}

func sortedUnique(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
