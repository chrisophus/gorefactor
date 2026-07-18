package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
)

// Two sensors born from a code review of this repo's own scars:
//
// generated-name catches committed functions still carrying the extractor's
// positional fallback name (extractBlockL<line>) — mechanical residue that
// should have been renamed or re-inlined before landing. The pattern is
// exact, so there are no false positives; the autofix-side nameability gate
// prevents new ones, and this rule ensures old ones can't accumulate
// silently.
//
// byvalue-buffer catches bytes.Buffer or strings.Builder parameters passed
// by value: every write inside the callee lands in a copy and is silently
// lost to the caller. A mechanical extraction once did exactly this to the
// add-test scaffold generator and shipped a generator that emitted empty
// test bodies — the fix compiles, tests that assert substrings still pass,
// and only the behavior is gone. Pass *bytes.Buffer / *strings.Builder.

var generatedNamePattern = regexp.MustCompile(`^extractBlockL[0-9]+$`)

type generatedNameRule struct{}

func (generatedNameRule) Name() string { return "generated-name" }

func (r generatedNameRule) Run(ctx LintContext) []lintIssue {
	var out []lintIssue
	for _, f := range ctx.Files {
		fset := token.NewFileSet()
		astFile, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			continue
		}
		for _, decl := range astFile.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || !generatedNamePattern.MatchString(fn.Name.Name) {
				continue
			}
			out = append(out, lintIssue{
				File:     f,
				Rule:     "generated-name",
				Severity: "warning",
				Message: fmt.Sprintf("%s carries the extractor's positional fallback name (line %d) — re-inline it into its caller or give it a real name",
					fn.Name.Name, fset.Position(fn.Pos()).Line),
			})
		}
	}
	return out
}

type byValueBufferRule struct{}

func (byValueBufferRule) Name() string { return "byvalue-buffer" }

func (r byValueBufferRule) Run(ctx LintContext) []lintIssue {
	var out []lintIssue
	for _, f := range ctx.Files {
		fset := token.NewFileSet()
		astFile, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			continue
		}
		for _, decl := range astFile.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Type.Params == nil {
				continue
			}
			for _, p := range fn.Type.Params.List {
				if name := byValueWriterType(p.Type); name != "" {
					out = append(out, lintIssue{
						File:     f,
						Rule:     "byvalue-buffer",
						Severity: "warning",
						Message: fmt.Sprintf("%s takes %s by value (line %d) — writes inside the function are lost to the caller; pass *%s",
							fn.Name.Name, name, fset.Position(fn.Pos()).Line, name),
					})
				}
			}
		}
	}
	return out
}

// byValueWriterType reports the qualified name when expr is a bare (non
// pointer) bytes.Buffer or strings.Builder, else "".
func byValueWriterType(expr ast.Expr) string {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return ""
	}
	switch {
	case pkg.Name == "bytes" && sel.Sel.Name == "Buffer":
		return "bytes.Buffer"
	case pkg.Name == "strings" && sel.Sel.Name == "Builder":
		return "strings.Builder"
	}
	return ""
}
