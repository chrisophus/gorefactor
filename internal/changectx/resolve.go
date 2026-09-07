package changectx

import (
	"fmt"
	"go/ast"
	goparser "go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/chrisophus/gorefactor/internal/goload"

	"golang.org/x/tools/go/packages"
)

// index is the loaded module: every package variant the loader produced, and
// a lookup from an absolute file path to the one package variant that owns it.
// Test variants re-list the same files, so the owner is picked by sorted
// package ID to keep one file mapping to one syntax tree on every run.
type index struct {
	fset     *token.FileSet
	pkgs     []*packages.Package
	byAbs    map[string]*fileUnit
	fallback *token.FileSet
}

// fileUnit is one Go file with whatever the loader could give for it. pkg and
// syntax are nil for a file that failed to load, which is the case the caller
// keeps working through.
type fileUnit struct {
	abs    string
	pkg    *packages.Package
	syntax *ast.File
	fset   *token.FileSet
}

// loadModule type-checks the module rooted at repo. Errors are returned as
// notes: a provider that fails entirely because one package is broken is
// useless on exactly the changes people most want reviewed.
func loadModule(repo string) (*index, []string) {
	idx := &index{byAbs: map[string]*fileUnit{}, fallback: token.NewFileSet()}
	pkgs, _, err := goload.LoadTypedPackages(filepath.Join(repo, "go.mod"), true)
	if err != nil {
		return idx, []string{fmt.Sprintf("module did not load: %v; symbol resolution is limited to syntax", err)}
	}
	sorted := append([]*packages.Package(nil), pkgs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	idx.pkgs = sorted

	var notes []string
	broken := 0
	for _, p := range sorted {
		if len(p.Errors) > 0 {
			broken++
		}
		if p.Fset != nil && idx.fset == nil {
			idx.fset = p.Fset
		}
		for i, f := range p.CompiledGoFiles {
			if i >= len(p.Syntax) {
				break
			}
			abs, aerr := filepath.Abs(f)
			if aerr != nil {
				continue
			}
			if _, seen := idx.byAbs[abs]; seen {
				continue
			}
			idx.byAbs[abs] = &fileUnit{abs: abs, pkg: p, syntax: p.Syntax[i], fset: p.Fset}
		}
	}
	if broken > 0 {
		notes = append(notes, fmt.Sprintf("%d package(s) failed to type-check; their callers and types were not resolved", broken))
		for _, msg := range goload.PackagesErrors(sorted) {
			notes = append(notes, "type-check: "+msg)
		}
	}
	if idx.fset == nil {
		idx.fset = idx.fallback
	}
	return idx, notes
}

// unit returns the loaded file, parsing it directly when the loader did not
// produce one. A file added but not yet compiling still gets its enclosing
// declarations mapped, without types.
func (idx *index) unit(abs string) *fileUnit {
	if u, ok := idx.byAbs[abs]; ok {
		return u
	}
	src, err := os.ReadFile(abs)
	if err != nil {
		return nil
	}
	syntax, perr := goparser.ParseFile(idx.fallback, abs, src, goparser.ParseComments)
	if syntax == nil && perr != nil {
		return nil
	}
	u := &fileUnit{abs: abs, syntax: syntax, fset: idx.fallback}
	idx.byAbs[abs] = u
	return u
}

// decl is one top-level declaration a change touched.
type decl struct {
	rel      string // repo-relative path of the file it lives in
	symbol   string // Name, or Receiver.Name for a method
	scope    string // directory-qualified symbol, opaque to the consumer
	kind     string // func, method, type, var, const
	receiver string
	start    int // first line, doc comment included
	end      int // last line
	exported bool
	changed  int // how many of its lines the diff touched
	obj      types.Object
	fn       *ast.FuncDecl
	unit     *fileUnit
}

// declsIn returns every top-level declaration in the file, in source order. A
// grouped var or const block yields one declaration per spec so a one-line
// change inside a fifty-entry block names the entry it touched.
func declsIn(u *fileUnit, rel, dir string) []*decl {
	if u == nil || u.syntax == nil {
		return nil
	}
	line := func(p token.Pos) int { return u.fset.Position(p).Line }
	var out []*decl
	for _, d := range u.syntax.Decls {
		switch n := d.(type) {
		case *ast.FuncDecl:
			start := n.Pos()
			if n.Doc != nil {
				start = n.Doc.Pos()
			}
			recv := receiverName(n)
			name := n.Name.Name
			kind := "func"
			if recv != "" {
				name = recv + "." + n.Name.Name
				kind = "method"
			}
			out = append(out, &decl{
				rel: rel, symbol: name, scope: qualify(dir, name), kind: kind,
				receiver: recv, start: line(start), end: line(n.End()),
				exported: n.Name.IsExported(), fn: n, unit: u,
			})
		case *ast.GenDecl:
			if n.Tok == token.IMPORT {
				continue
			}
			out = append(out, genDecls(u, n, rel, dir, line)...)
		}
	}
	return out
}

func genDecls(u *fileUnit, n *ast.GenDecl, rel, dir string, line func(token.Pos) int) []*decl {
	kind := strings.ToLower(n.Tok.String())
	groupStart := n.Pos()
	if n.Doc != nil {
		groupStart = n.Doc.Pos()
	}
	var out []*decl
	for _, spec := range n.Specs {
		for _, name := range specNames(spec) {
			start, end := line(spec.Pos()), line(spec.End())
			if len(n.Specs) == 1 {
				start, end = line(groupStart), line(n.End())
			} else if doc := specDoc(spec); doc != nil {
				start = line(doc.Pos())
			}
			out = append(out, &decl{
				rel: rel, symbol: name, scope: qualify(dir, name), kind: kind,
				start: start, end: end, exported: ast.IsExported(name), unit: u,
			})
		}
	}
	return out
}

func specNames(spec ast.Spec) []string {
	switch s := spec.(type) {
	case *ast.TypeSpec:
		return []string{s.Name.Name}
	case *ast.ValueSpec:
		var out []string
		for _, n := range s.Names {
			out = append(out, n.Name)
		}
		return out
	}
	return nil
}

func specDoc(spec ast.Spec) *ast.CommentGroup {
	switch s := spec.(type) {
	case *ast.TypeSpec:
		return s.Doc
	case *ast.ValueSpec:
		return s.Doc
	}
	return nil
}

// resolveObject attaches the type-checker object for a declaration. It stays
// nil when the package did not type-check, and every stage that needs an
// object skips such a declaration rather than guessing by name.
func resolveObject(d *decl) {
	if d.unit == nil || d.unit.pkg == nil || d.unit.pkg.TypesInfo == nil {
		return
	}
	if d.fn != nil {
		if obj, ok := d.unit.pkg.TypesInfo.Defs[d.fn.Name]; ok {
			d.obj = obj
		}
		return
	}
	if d.unit.pkg.Types == nil {
		return
	}
	// Anything else a changed hunk can enclose is a top-level type, var, or
	// const, and those all live in the package scope under their own name.
	d.obj = d.unit.pkg.Types.Scope().Lookup(d.symbol)
}

func receiverName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return ""
	}
	return goload.ReceiverTypeName(fn.Recv.List[0].Type)
}

// qualify prefixes a symbol with the directory it lives in. The consumer
// treats the result as an opaque string; the shape only has to be stable and
// readable.
func qualify(dir, symbol string) string {
	if dir == "" || dir == "." {
		return symbol
	}
	return dir + "." + symbol
}

// overlaps reports whether a declaration covers any part of a changed span.
func (d *decl) overlaps(r lineRange) bool {
	return d.start <= r.end && r.start <= d.end
}

// countChanged sums the lines of a declaration the diff touched. It feeds the
// priority hint, where a heavily rewritten function outranks a one-line edit.
func (d *decl) countChanged(ranges []lineRange) int {
	total := 0
	for _, r := range ranges {
		lo, hi := max(r.start, d.start), min(r.end, d.end)
		if lo <= hi {
			total += hi - lo + 1
		}
	}
	return total
}
