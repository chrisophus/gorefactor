package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
)

// stranded-comment (harness-integrity plan item 1) catches a doc comment
// whose leading identifier names a different top-level declaration in the
// package than the one it precedes — the exact failure mode a historical
// mechanical edit left in orchestrator/types.go, where declarations were
// moved out from under their comments. The heuristic is deliberately
// precise so it stays silent on healthy code: it fires only when the first
// word of the doc comment (1) is a Go identifier, (2) is not a name
// declared by the commented declaration, and (3) exactly names some other
// top-level declaration in the same package. Go doc style ("Foo does...")
// makes a doc comment opening with a sibling's name a near-certain sign the
// comment and its declaration have been separated. Test files are excluded:
// test doc comments conventionally open with the name of the function under
// test ("Foo mirrors the fixer: ..."), which is exactly the shape this rule
// flags. (Comment-only edit rationale: gorefactor has no op for free-floating
// comments, so this block is maintained by hand.)

type strandedCommentRule struct{}

func (strandedCommentRule) Name() string { return "stranded-comment" }

func (r strandedCommentRule) Run(ctx LintContext) []lintIssue {
	byDir := make(map[string][]string)
	for _, f := range ctx.Files {
		if isTestFile(f) {
			continue
		}
		dir := filepath.Dir(f)
		byDir[dir] = append(byDir[dir], f)
	}
	var out []lintIssue
	for _, files := range byDir {
		out = append(out, strandedCommentsInPackage(files)...)
	}
	return out
}

// strandedCommentsInPackage parses every file of one package directory,
// collects all top-level declared names, then flags doc comments that open
// with a sibling declaration's name instead of their own.
func strandedCommentsInPackage(files []string) []lintIssue {
	type parsedFile struct {
		path string
		fset *token.FileSet
		ast  *ast.File
	}
	var parsed []parsedFile
	pkgNames := make(map[string]bool)
	for _, f := range files {
		fset := token.NewFileSet()
		astFile, err := parser.ParseFile(fset, f, nil, parser.ParseComments)
		if err != nil {
			continue
		}
		parsed = append(parsed, parsedFile{path: f, fset: fset, ast: astFile})
		for _, decl := range astFile.Decls {
			for name := range topLevelDeclNames(decl) {
				pkgNames[name] = true
			}
		}
	}
	var out []lintIssue
	for _, pf := range parsed {
		for _, decl := range pf.ast.Decls {
			own := topLevelDeclNames(decl)
			for _, dc := range docCommentsOf(decl) {
				first := leadingIdent(dc.doc.Text())
				if first == "" || own[first] || dc.own[first] || !pkgNames[first] {
					continue
				}
				out = append(out, lintIssue{
					File:     pf.path,
					Rule:     "stranded-comment",
					Severity: "warning",
					Message: fmt.Sprintf("doc comment at line %d opens with %q but documents %s — the comment appears stranded from the declaration it describes",
						pf.fset.Position(dc.doc.Pos()).Line, first, dc.subject),
				})
			}
		}
		out = append(out, freeFloatingStranded(pf.path, pf.fset, pf.ast, pkgNames)...)
	}
	return out
}

// freeFloatingStranded flags comment groups attached to nothing — not a doc
// comment, not inside any declaration — whose leading identifier names a
// top-level declaration in the package. That is the residue a mechanical
// body-extraction leaves behind: "// executeMoveMethod executes ..." floating
// between functions while executeMoveMethod lives in another file. (This
// repo's orchestrator/ carried 25 lines of exactly that shape while the
// doc-comment-only version of this rule stayed silent.) Narration comments
// inside function bodies are never visited: any group positioned within a
// declaration's span is skipped.
func freeFloatingStranded(path string, fset *token.FileSet, file *ast.File, pkgNames map[string]bool) []lintIssue {
	attached := make(map[*ast.CommentGroup]bool)
	if file.Doc != nil {
		attached[file.Doc] = true
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch d := n.(type) {
		case *ast.FuncDecl:
			if d.Doc != nil {
				attached[d.Doc] = true
			}
		case *ast.GenDecl:
			if d.Doc != nil {
				attached[d.Doc] = true
			}
		case *ast.TypeSpec:
			if d.Doc != nil {
				attached[d.Doc] = true
			}
			if d.Comment != nil {
				attached[d.Comment] = true
			}
		case *ast.ValueSpec:
			if d.Doc != nil {
				attached[d.Doc] = true
			}
			if d.Comment != nil {
				attached[d.Comment] = true
			}
		case *ast.Field:
			if d.Doc != nil {
				attached[d.Doc] = true
			}
			if d.Comment != nil {
				attached[d.Comment] = true
			}
		case *ast.ImportSpec:
			if d.Doc != nil {
				attached[d.Doc] = true
			}
			if d.Comment != nil {
				attached[d.Comment] = true
			}
		}
		return true
	})
	var out []lintIssue
	for _, cg := range file.Comments {
		if attached[cg] || insideAnyDecl(cg, file.Decls) {
			continue
		}
		first := leadingIdent(cg.Text())
		if first == "" || !pkgNames[first] {
			continue
		}
		out = append(out, lintIssue{
			File:     path,
			Rule:     "stranded-comment",
			Severity: "warning",
			Message: fmt.Sprintf("free-floating comment at line %d opens with %q, which names a declaration elsewhere in the package — likely narration stranded by a mechanical edit; delete it or reattach it",
				fset.Position(cg.Pos()).Line, first),
		})
	}
	return out
}

// insideAnyDecl reports whether a comment group lies within the source span
// of any top-level declaration (i.e. it is body/inline narration, which this
// rule deliberately leaves alone).
func insideAnyDecl(cg *ast.CommentGroup, decls []ast.Decl) bool {
	for _, d := range decls {
		if cg.Pos() >= d.Pos() && cg.End() <= d.End() {
			return true
		}
	}
	return false
}

// docComment pairs a doc comment group with the names its immediate subject
// declares and a human-readable subject label for the finding message.
type docComment struct {
	doc     *ast.CommentGroup
	own     map[string]bool
	subject string
}

// docCommentsOf returns the doc comments attached to a top-level declaration:
// the declaration-level doc, plus per-spec docs inside a grouped GenDecl.
func docCommentsOf(decl ast.Decl) []docComment {
	var out []docComment
	switch d := decl.(type) {
	case *ast.FuncDecl:
		if d.Doc != nil {
			out = append(out, docComment{doc: d.Doc, own: map[string]bool{d.Name.Name: true}, subject: "func " + d.Name.Name})
		}
	case *ast.GenDecl:
		if d.Doc != nil {
			out = append(out, docComment{doc: d.Doc, own: topLevelDeclNames(d), subject: genDeclSubject(d)})
		}
		for _, spec := range d.Specs {
			names := specNames(spec)
			var doc *ast.CommentGroup
			switch s := spec.(type) {
			case *ast.TypeSpec:
				doc = s.Doc
			case *ast.ValueSpec:
				doc = s.Doc
			}
			if doc != nil {
				out = append(out, docComment{doc: doc, own: names, subject: genDeclSubject(d)})
			}
		}
	}
	return out
}

// genDeclSubject renders a short label like "type Foo" or "var a, b" for a
// GenDecl so findings read naturally.
func genDeclSubject(d *ast.GenDecl) string {
	names := make([]string, 0, len(d.Specs))
	for _, spec := range d.Specs {
		for n := range specNames(spec) {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		return d.Tok.String()
	}
	if len(names) > 3 {
		names = append(names[:3], "...")
	}
	return d.Tok.String() + " " + strings.Join(names, ", ")
}

// topLevelDeclNames returns every name a top-level declaration introduces
// (function/method names, type names, package-level var/const names).
func topLevelDeclNames(decl ast.Decl) map[string]bool {
	names := make(map[string]bool)
	switch d := decl.(type) {
	case *ast.FuncDecl:
		names[d.Name.Name] = true
	case *ast.GenDecl:
		for _, spec := range d.Specs {
			for n := range specNames(spec) {
				names[n] = true
			}
		}
	}
	return names
}

// specNames returns the names one GenDecl spec declares.
func specNames(spec ast.Spec) map[string]bool {
	names := make(map[string]bool)
	switch s := spec.(type) {
	case *ast.TypeSpec:
		names[s.Name.Name] = true
	case *ast.ValueSpec:
		for _, n := range s.Names {
			names[n.Name] = true
		}
	}
	return names
}

// leadingIdent extracts the leading Go-identifier run from cleaned doc-comment
// text, or "" when the text does not open with an identifier. "Package ..."
// openers are file-level prose, not a declaration reference, and directive
// lines never reach here (comment cleaning strips them).
func leadingIdent(text string) string {
	text = strings.TrimSpace(text)
	end := 0
	for end < len(text) {
		b := text[end]
		if b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_' {
			end++
			continue
		}
		break
	}
	if end == 0 || (text[0] >= '0' && text[0] <= '9') {
		return ""
	}
	word := text[:end]
	if word == "Package" || word == "Deprecated" {
		return ""
	}
	return word
}
