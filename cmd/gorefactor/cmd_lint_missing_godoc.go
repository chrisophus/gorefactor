package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

type missingGodocRule struct{}

func (missingGodocRule) Name() string { return "missing-godoc" }

func (r missingGodocRule) Run(ctx LintContext) []lintIssue {
	var out []lintIssue
	for _, f := range ctx.Files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		issues, err := checkMissingGodoc(f)
		if err != nil {
			continue
		}
		out = append(out, issues...)
	}
	return out
}

// checkMissingGodoc inspects all exported top-level declarations in a file and
// returns an info-severity issue for each one that lacks a doc comment.
func checkMissingGodoc(file string) ([]lintIssue, error) {
	fset := token.NewFileSet()
	astFile, err := parser.ParseFile(fset, file, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	var out []lintIssue
	for _, decl := range astFile.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if !d.Name.IsExported() {
				continue
			}
			if d.Doc == nil || d.Doc.Text() == "" {
				line := fset.Position(d.Pos()).Line
				out = append(out, lintIssue{
					File:       file,
					Rule:       "missing-godoc",
					Severity:   "info",
					Message:    fmt.Sprintf("%s: exported function %s is missing a doc comment (line %d)", file, d.Name.Name, line),
					AutoFixCmd: fmt.Sprintf("gorefactor set-doc %s %s -", file, d.Name.Name),
				})
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					if !s.Name.IsExported() {
						continue
					}
					// A GenDecl may carry the doc comment on itself (single-spec)
					// or on each spec (multi-spec). Check both.
					hasDoc := (d.Doc != nil && d.Doc.Text() != "") ||
						(s.Comment != nil && s.Comment.Text() != "")
					if !hasDoc {
						line := fset.Position(s.Pos()).Line
						out = append(out, lintIssue{
							File:       file,
							Rule:       "missing-godoc",
							Severity:   "info",
							Message:    fmt.Sprintf("%s: exported type %s is missing a doc comment (line %d)", file, s.Name.Name, line),
							AutoFixCmd: fmt.Sprintf("gorefactor set-doc %s %s -", file, s.Name.Name),
						})
					}
				case *ast.ValueSpec:
					// Only flag the first name in the spec to avoid noise on
					// grouped consts/vars where the doc is on the GenDecl.
					if len(s.Names) == 0 || !s.Names[0].IsExported() {
						continue
					}
					hasDoc := (d.Doc != nil && d.Doc.Text() != "") ||
						(s.Comment != nil && s.Comment.Text() != "")
					if !hasDoc {
						line := fset.Position(s.Pos()).Line
						name := s.Names[0].Name
						out = append(out, lintIssue{
							File:       file,
							Rule:       "missing-godoc",
							Severity:   "info",
							Message:    fmt.Sprintf("%s: exported symbol %s is missing a doc comment (line %d)", file, name, line),
							AutoFixCmd: fmt.Sprintf("gorefactor set-doc %s %s -", file, name),
						})
					}
				}
			}
		}
	}
	return out, nil
}
