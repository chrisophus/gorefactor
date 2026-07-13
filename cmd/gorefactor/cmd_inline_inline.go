package main

import (
	"fmt"
	"go/ast"
	goparser "go/parser"
	"go/token"
	"os"
	"strings"

	"github.com/chrisophus/gorefactor/orchestrator"
)

// inlineCommand inlines a function into all its call sites and deletes it.
// MVP scope (a harness refuses rather than corrupts):
//   - the body is a single `return <expr>` or a statement list with no returns
//   - all call sites are in the same package (plus a best-effort scan that
//     refuses when an exported function is referenced from another package)
//   - every argument is side-effect-free and every parameter is used at most
//     once (temp-var introduction is out of scope)
//   - refused outright: multiple return values, named results, variadic or
//     generic functions, defer/go, closures, recursion, use as a value
func inlineCommand(args []string) error {
	pos, flags := parseFlags(args, inlineFlags)
	if len(pos) < 2 {
		return usageErrorf("usage: inline <file> <Func>")
	}
	file := pos[0]
	funcName := pos[1]
	if strings.Contains(funcName, ":") {
		return usageErrorf("inline supports top-level functions only, not methods (got %q)", funcName)
	}

	m := &mutation{op: "inline", file: file, files: packageGoFiles(file)}
	m.setCommonFlags(flags)

	declSrc, err := os.ReadFile(file)
	if err != nil {
		return m.fail(err)
	}
	fset := token.NewFileSet()
	node, err := goparser.ParseFile(fset, file, declSrc, goparser.ParseComments)
	if err != nil {
		return m.fail(parseErrorf("failed to parse %s: %v", file, err))
	}

	target := findInlineTarget(node, funcName)
	if target == nil {
		funcs, _ := declNames(node)
		return m.fail(notFoundError(
			fmt.Sprintf("function %q not found in %s", funcName, file),
			funcName, funcs))
	}

	tmpl, err := buildInlineTemplate(fset, declSrc, target)
	if err != nil {
		return m.fail(err)
	}

	hasResults := target.Type.Results != nil && len(target.Type.Results.List) > 0
	sites, err := collectInlineCallSites(file, node.Name.Name, funcName, hasResults, len(tmpl.params))
	if err != nil {
		return m.fail(err)
	}
	if ast.IsExported(funcName) {
		if loc := findCrossPackageUse(file, node.Name.Name, funcName); loc != "" {
			return m.fail(notFoundErrorf(
				"cannot inline %s: referenced outside its package at %s (all call sites must be in the same package)",
				funcName, loc))
		}
	}

	// Validate arguments and parameter usage per site.
	for _, s := range sites {
		for i, arg := range s.call.Args {
			if !isPureExpr(arg) {
				return m.fail(parseErrorf(
					"cannot inline %s: argument %d at %s:%d may have side effects; temp vars are out of scope — simplify the argument first",
					funcName, i+1, s.file, s.line))
			}
		}
	}

	// Build per-file edit lists.
	edits := map[string][]inlineTextEdit{}
	extractBlockL86(sites, tmpl, edits)

	// Delete the declaration (including its doc comment).
	delStart := buildInlineEdits(fset, target)
	if target.Doc != nil {
		delStart = fset.Position(target.Doc.Pos()).Offset
	}
	delEnd := fset.Position(target.End()).Offset
	for delEnd < len(declSrc) && declSrc[delEnd] == '\n' {
		delEnd++
	}
	edits[file] = append(edits[file], inlineTextEdit{start: delStart, end: delEnd, text: ""})

	// Apply edits in memory, parse-verify every file, then write.
	results := map[string][]byte{}
	if r0, done := extractBlockL120(edits, m, funcName, results); done {
		return r0
	}

	return m.run(func() (string, error) {
		for f, out := range results {
			if err := os.WriteFile(f, out, 0644); err != nil {
				return "", err
			}
			if err := orchestrator.FormatImports(f); err != nil {
				fmt.Fprintf(os.Stderr, "warning: format imports on %s: %v\n", f, err)
			}
		}
		return fmt.Sprintf("Inlined %s into %d call site(s) and deleted it from %s", funcName, len(sites), file), nil
	})
}

func extractBlockL86(sites []inlineCallSite, tmpl *inlineTemplate, edits map[string][]inlineTextEdit) {
	for _, s := range sites {
		argTexts := make([]string, len(s.call.Args))
		for i := range s.call.Args {
			argTexts[i] = string(s.src[s.argStart[i]:s.argEnd[i]])
		}
		text := tmpl.substitute(argTexts)
		start, end := s.start, s.end
		if tmpl.exprMode {
			if s.stmtStart >= 0 {

				start, end = s.stmtStart, s.stmtEnd
				text = "_ = " + text
			} else if !isSimpleExprText(tmpl.returnExpr) {
				text = "(" + text + ")"
			}
		} else {
			start, end = s.stmtStart, s.stmtEnd
		}
		edits[s.file] = append(edits[s.file], inlineTextEdit{start: start, end: end, text: text})
	}
}

func extractBlockL120(edits map[string][]inlineTextEdit, m *mutation, funcName string, results map[string][]byte) (r0 error, done bool) {
	for f, list := range edits {
		src, err := os.ReadFile(f)
		if err != nil {
			return m.fail(err), true
		}
		out, err := applyInlineEdits(src, list)
		if err != nil {
			return m.fail(err), true
		}
		if _, perr := goparser.ParseFile(token.NewFileSet(), f, out, 0); perr != nil {
			return m.fail(parseErrorf("inlining %s would produce a malformed file %s: %v", funcName, f, perr)), true
		}
		results[f] = out
	}
	return
}

func buildInlineEdits(fset *token.FileSet, target *ast.FuncDecl) int {
	delStart := fset.Position(target.Pos()).Offset
	return delStart
}

func findInlineTarget(node *ast.File, funcName string) *ast.FuncDecl {
	var target *ast.FuncDecl
	for _, d := range node.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && fd.Name.Name == funcName {
			target = fd
			break
		}
	}
	return target
}
