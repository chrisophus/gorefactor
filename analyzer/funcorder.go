package analyzer

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"sort"
)

// FuncorderIssue describes a violation of funcorder's declaration-ordering rules: a struct's
// constructor must be declared before any of its methods (autofix places it immediately after the
// struct), a struct's exported methods must all precede its unexported methods, and top-level
// receiver-less functions (excluding init() and tracked constructors) must have exported ones
// before unexported ones — all in file declaration order.
//
// Detection is file-local: a struct's methods declared in other files of the same package are out
// of scope, matching how `split` operates file-locally.
type FuncorderIssue struct {
	File       string
	Line       int
	Column     int
	Rule       string
	Message    string
	StructName string
	FuncName   string
}

const (
	funcorderConstructorRuleName  = "funcorder-constructor"
	funcorderStructMethodRuleName = "funcorder-struct-method"
	funcorderFunctionRuleName     = "funcorder-function"
)

// FileFuncorderIssues parses file and reports funcorder-constructor, funcorder-struct-method, and
// funcorder-function violations.
func FileFuncorderIssues(file string) ([]FuncorderIssue, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	return funcorderIssuesForFile(f, fset, file), nil
}

// ApplyFuncorderFixes rewrites the top-level declaration order of file to
// satisfy funcorder's constructor, struct-method, and loose-function
// placement rules:
//
//	struct type decl -> constructor(s) (original relative order) ->
//	exported methods (original relative order) -> unexported methods
//	(original relative order)
//
// and, independently, top-level functions that aren't methods, `init()`, or
// a tracked constructor are reordered so exported ones precede unexported
// ones (each subgroup keeping its original relative order).
//
// Declarations unrelated to any struct group or loose-function slot keep
// their original relative order and position untouched. Returns the
// rewritten, gofmt'd source and the number of fixes actually applied
// (struct groups reordered, plus 1 if the loose-function pass changed
// anything).
func ApplyFuncorderFixes(filename string, src []byte) ([]byte, int, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, src, parser.ParseComments)
	if err != nil {
		return nil, 0, fmt.Errorf("parse %s: %w", filename, err)
	}
	groups := funcorderGroups(f)

	// Build the desired index permutation per group; count only groups that
	// actually need reordering.
	newOrder, changed := funcorderStructOrder(f, groups)

	// Second pass: reorder loose top-level functions (excluding methods,
	// init, and tracked constructors — those are covered by the group pass
	// above) so exported functions precede unexported ones. Only the decl
	// occupying each loose-function slot in newOrder changes; every other
	// slot (struct groups, types, vars, imports) keeps its exact position.
	changed = funcorderReorderLooseFuncs(groups, newOrder, f, changed)

	if changed == 0 {
		return src, 0, nil
	}

	buf := funcorderRenderReordered(f, fset, src, newOrder)

	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, 0, fmt.Errorf("internal: funcorder rewrite produced unparsable Go: %w", err)
	}
	return formatted, changed, nil
}

// funcorderGroup captures one struct's constructor(s) and methods, along
// with their Decls-index positions, for both detection and rewriting.
type funcorderGroup struct {
	structName  string
	structIdx   int
	structDecl  *ast.GenDecl
	ctorIdx     []int // Decls indices of constructor FuncDecls, in decl order
	exportedIdx []int // Decls indices of exported methods, in decl order
	unexpIdx    []int // Decls indices of unexported methods, in decl order
}

// issues computes the violations for this group.
func (g *funcorderGroup) issues(fset *token.FileSet, filename string) []FuncorderIssue {
	var out []FuncorderIssue

	// funcorder-constructor: every constructor index must be strictly
	// between the struct's index and every method's index.
	out = funcorderConstructorIssue(g, fset, out, filename)

	// funcorder-struct-method: no unexported method index may be less than
	// any exported method index.
	out = funcorderStructMethodIssue(g, fset, out, filename)
	return out
}

// desiredOrder returns this group's member Decls-indices (struct, then
// constructors, then exported methods, then unexported methods) in the
// order they should appear.
func (g *funcorderGroup) desiredOrder() []int {
	out := make([]int, 0, 1+len(g.ctorIdx)+len(g.exportedIdx)+len(g.unexpIdx))
	out = append(out, g.structIdx)
	out = append(out, sortedCopy(g.ctorIdx)...)
	out = append(out, sortedCopy(g.exportedIdx)...)
	out = append(out, sortedCopy(g.unexpIdx)...)
	return out
}

// allMemberIdx returns this group's member indices in their *original*
// file order, for comparison against desiredOrder.
func (g *funcorderGroup) allMemberIdx() []int {
	out := make([]int, 0, 1+len(g.ctorIdx)+len(g.exportedIdx)+len(g.unexpIdx))
	out = append(out, g.structIdx)
	out = append(out, g.ctorIdx...)
	out = append(out, g.exportedIdx...)
	out = append(out, g.unexpIdx...)
	sort.Ints(out)
	return out
}

func funcorderIssuesForFile(f *ast.File, fset *token.FileSet, filename string) []FuncorderIssue {
	groups := funcorderGroups(f)
	var out []FuncorderIssue
	for _, g := range groups {
		out = append(out, g.issues(fset, filename)...)
	}
	if iss, ok := funcorderFunctionIssue(f, fset, filename, groups); ok {
		out = append(out, iss)
	}
	return out
}

// funcorderGroups finds every top-level struct declared in f and collects
// its constructor candidates and methods.
func funcorderGroups(f *ast.File) []*funcorderGroup {
	var groups []*funcorderGroup
	byName := make(map[string]*funcorderGroup)

	for i, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			if _, ok := ts.Type.(*ast.StructType); !ok {
				continue
			}
			g := &funcorderGroup{structName: ts.Name.Name, structIdx: i, structDecl: gd}
			groups = append(groups, g)
			byName[ts.Name.Name] = g
		}
	}
	if len(groups) == 0 {
		return nil
	}

	funcorderAssignMembers(f, byName)
	return groups
}

func funcorderAssignMembers(f *ast.File, byName map[string]*funcorderGroup) {
	for i, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if fn.Recv == nil || len(fn.Recv.List) == 0 {
			if name, ok := funcorderConstructorTarget(fn); ok {
				if g, ok := byName[name]; ok {
					g.ctorIdx = append(g.ctorIdx, i)
				}
			}
			continue
		}
		recvName := funcorderReceiverTypeName(fn.Recv.List[0].Type)
		g, ok := byName[recvName]
		if !ok {
			continue
		}
		if fn.Name.IsExported() {
			g.exportedIdx = append(g.exportedIdx, i)
		} else {
			g.unexpIdx = append(g.unexpIdx, i)
		}
	}
}

// funcorderConstructorTarget reports whether fn looks like a constructor
// (name prefixed New/Must) and, if so, the struct name it constructs, based
// on its result list containing that identifier (value or pointer).
func funcorderConstructorTarget(fn *ast.FuncDecl) (string, bool) {
	name := fn.Name.Name
	var prefix string
	switch {
	case hasPrefixLen(name, "New") && len(name) > len("New"):
		prefix = "New"
	case hasPrefixLen(name, "Must") && len(name) > len("Must"):
		prefix = "Must"
	default:
		return "", false
	}
	want := name[len(prefix):]
	if fn.Type.Results == nil {
		return "", false
	}
	for _, res := range fn.Type.Results.List {
		if funcorderTypeName(res.Type) == want {
			return want, true
		}
	}
	return "", false
}

func hasPrefixLen(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func funcorderTypeName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return funcorderTypeName(t.X)
	default:
		return ""
	}
}

func funcorderReceiverTypeName(e ast.Expr) string {
	return funcorderTypeName(e)
}

func funcorderStructMethodIssue(g *funcorderGroup, fset *token.FileSet, out []FuncorderIssue, filename string) []FuncorderIssue {
	if len(g.unexpIdx) > 0 && len(g.exportedIdx) > 0 {
		maxExported := g.exportedIdx[0]
		for _, idx := range g.exportedIdx {
			if idx > maxExported {
				maxExported = idx
			}
		}
		minUnexported := g.unexpIdx[0]
		for _, idx := range g.unexpIdx {
			if idx < minUnexported {
				minUnexported = idx
			}
		}
		if minUnexported < maxExported {
			pos := fset.Position(g.structDecl.Pos())
			out = append(out, FuncorderIssue{
				File:       filename,
				Line:       pos.Line,
				Column:     pos.Column,
				Rule:       funcorderStructMethodRuleName,
				Message:    fmt.Sprintf("exported methods of %s must all be declared before its unexported methods", g.structName),
				StructName: g.structName,
			})
		}
	}
	return out
}

// declSpan holds the source text (including any owning doc comment) for one
// top-level declaration, keyed by its original Decls index.
type declSpan struct {
	origIdx int
	text    string
}

func funcorderConstructorIssue(g *funcorderGroup, fset *token.FileSet, out []FuncorderIssue, filename string) []FuncorderIssue {
	minMethodIdx := -1
	for _, idx := range append(append([]int{}, g.exportedIdx...), g.unexpIdx...) {
		if minMethodIdx == -1 || idx < minMethodIdx {
			minMethodIdx = idx
		}
	}
	for _, ci := range g.ctorIdx {
		ok := ci > g.structIdx
		if ok && minMethodIdx != -1 && ci > minMethodIdx {
			ok = false
		}
		if !ok {
			pos := fset.Position(g.structDecl.Pos())
			out = append(out, FuncorderIssue{
				File:       filename,
				Line:       pos.Line,
				Column:     pos.Column,
				Rule:       funcorderConstructorRuleName,
				Message:    fmt.Sprintf("constructor for %s must be declared before its methods", g.structName),
				StructName: g.structName,
			})
			break
		}
	}
	return out
}

func funcorderRenderReordered(f *ast.File, fset *token.FileSet, src []byte, newOrder []int) bytes.Buffer {
	spans := buildDeclSpans(f, fset, src)
	spanByIdx := make(map[int]declSpan, len(spans))
	for _, s := range spans {
		spanByIdx[s.origIdx] = s
	}
	var buf bytes.Buffer
	headerEnd := fset.Position(f.Name.End()).Offset
	buf.Write(src[:headerEnd])
	buf.WriteString("\n")
	for i, idx := range newOrder {
		if i > 0 {
			buf.WriteString("\n\n")
		}
		buf.WriteString(spanByIdx[idx].text)
	}
	buf.WriteString("\n")
	return buf
}

func funcorderReorderLooseFuncs(groups []*funcorderGroup, newOrder []int, f *ast.File, changed int) int {
	ctorIdx := funcorderCtorIdxSet(groups)
	var loosePositions, looseDeclIdx []int
	for pos, idx := range newOrder {
		fn, ok := f.Decls[idx].(*ast.FuncDecl)
		if !ok || (fn.Recv != nil && len(fn.Recv.List) > 0) || fn.Name.Name == "init" || ctorIdx[idx] {
			continue
		}
		loosePositions = append(loosePositions, pos)
		looseDeclIdx = append(looseDeclIdx, idx)
	}
	if len(looseDeclIdx) >= 2 {
		reordered := funcorderStablePartitionExported(f, looseDeclIdx)
		if !sameOrder(reordered, looseDeclIdx) {
			changed++
			for i, pos := range loosePositions {
				newOrder[pos] = reordered[i]
			}
		}
	}
	return changed
}

func funcorderStructOrder(f *ast.File, groups []*funcorderGroup) ([]int, int) {
	newOrder := make([]int, 0, len(f.Decls))
	placed := make(map[int]bool, len(f.Decls))
	changed := 0
	groupsByStructIdx := make(map[int][]*funcorderGroup, len(groups))
	for _, g := range groups {
		groupsByStructIdx[g.structIdx] = append(groupsByStructIdx[g.structIdx], g)
	}
	for i := range f.Decls {
		if placed[i] {
			continue
		}
		gs, isStruct := groupsByStructIdx[i]
		if !isStruct {
			newOrder = append(newOrder, i)
			placed[i] = true
			continue
		}
		newOrder = append(newOrder, i)
		placed[i] = true
		for _, g := range gs {
			desired := g.desiredOrder()
			if !sameOrder(desired, g.allMemberIdx()) {
				changed++
			}
			for _, idx := range desired {
				if !placed[idx] {
					newOrder = append(newOrder, idx)
					placed[idx] = true
				}
			}
		}
	}
	for i := range f.Decls {
		if !placed[i] {
			newOrder = append(newOrder, i)
			placed[i] = true
		}
	}
	return newOrder, changed
}

func sortedCopy(idx []int) []int {
	out := append([]int{}, idx...)
	sort.Ints(out)
	return out
}

func sameOrder(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// buildDeclSpans computes, for every top-level decl in f, the source text
// span including any owning leading doc comment (mirrors the boundary rule
// used by executeMoveMethod / commentBelongsToDecl in orchestrator).
func buildDeclSpans(f *ast.File, fset *token.FileSet, src []byte) []declSpan {
	spans := make([]declSpan, len(f.Decls))
	for i, decl := range f.Decls {
		start := fset.Position(decl.Pos()).Offset
		end := fset.Position(decl.End()).Offset
		for _, cg := range f.Comments {
			if funcorderCommentBelongsToDecl(fset, decl.Pos(), decl.End(), cg) {
				if off := fset.Position(cg.Pos()).Offset; off < start {
					start = off
				}
			}
		}
		text := string(bytes.TrimSpace(src[start:end]))
		spans[i] = declSpan{origIdx: i, text: text}
	}
	return spans
}

// funcorderCommentBelongsToDecl mirrors orchestrator.commentBelongsToDecl:
// a comment belongs to a decl if it's inside the decl's token range, or
// ends within one blank line above the decl's start.
func funcorderCommentBelongsToDecl(fset *token.FileSet, declStart, declEnd token.Pos, cg *ast.CommentGroup) bool {
	if cg.Pos() >= declStart && cg.End() <= declEnd {
		return true
	}
	declLine := fset.Position(declStart).Line
	cgEndLine := fset.Position(cg.End()).Line
	if declLine > cgEndLine && (declLine-cgEndLine) <= 2 {
		return true
	}
	return false
}
