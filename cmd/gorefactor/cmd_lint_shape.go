package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// Shape-conditioned rules (docs/doctor-design-plan.md, step 4): severity
// depends on what kind of package the code lives in. The plan's example is
// os.Exit — a finding in a library, not in main.

// fatalCalls are the process-terminating log calls a library must not make:
// they take the decision to kill the process away from the caller.
var fatalCalls = map[string]bool{
	"Fatal": true, "Fatalf": true, "Fatalln": true,
}

type fatalInLibraryRule struct{}

func (fatalInLibraryRule) Name() string { return "fatal-in-library" }

// Run flags log.Fatal*/os.Exit (warning) and panic (info — sometimes a
// legitimate programmer-error signal, never a clean API) in non-main,
// non-test packages. Package main is exempt: terminating the process is a
// binary's prerogative.
func (r fatalInLibraryRule) Run(ctx LintContext) []lintIssue {
	var out []lintIssue
	for _, f := range ctx.Files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		astFile, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil || astFile.Name.Name == "main" {
			continue
		}
		ast.Inspect(astFile, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if iss, found := r.checkCall(call, f, fset); found {
				out = append(out, iss)
			}
			return true
		})
	}
	return out
}

// checkCall classifies one call expression, returning an issue when it is a
// process-terminating call that does not belong in a library package.
func (fatalInLibraryRule) checkCall(call *ast.CallExpr, file string, fset *token.FileSet) (lintIssue, bool) {
	line := fset.Position(call.Pos()).Line
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		if fn.Name == "panic" {
			return lintIssue{
				File:     file,
				Rule:     "fatal-in-library",
				Severity: "info",
				Message:  fmt.Sprintf("panic in library package (line %d) — return an error unless this is an unreachable programmer-error guard", line),
			}, true
		}
	case *ast.SelectorExpr:
		pkg, ok := fn.X.(*ast.Ident)
		if !ok {
			return lintIssue{}, false
		}
		if (pkg.Name == "log" && fatalCalls[fn.Sel.Name]) || (pkg.Name == "os" && fn.Sel.Name == "Exit") {
			return lintIssue{
				File:     file,
				Rule:     "fatal-in-library",
				Severity: "warning",
				Message:  fmt.Sprintf("%s.%s in library package (line %d) — a library must return errors, not kill the process; only package main decides to exit", pkg.Name, fn.Sel.Name, line),
			}, true
		}
	}
	return lintIssue{}, false
}
