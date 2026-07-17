package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// regexpHoistRule flags regexp compilation with a constant pattern inside a
// function body — recompiled on every call for no reason (the Go analog of
// react-doctor's hoist-RegExp rule; see the inventory in
// docs/doctor-design-plan.md). MustCompile sites get the hoist-regexp autofix;
// Compile sites only warn, since hoisting would move its error return.
type regexpHoistRule struct{}

func (regexpHoistRule) Name() string { return "regexp-compile-in-func" }

func (r regexpHoistRule) Run(ctx LintContext) []lintIssue {
	var out []lintIssue
	for _, f := range ctx.Files {
		if strings.HasSuffix(f, "_test.go") {
			continue // per-call compile cost is irrelevant in tests
		}
		fset := token.NewFileSet()
		astFile, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			continue
		}
		for _, decl := range astFile.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil || fd.Name.Name == "init" {
				continue
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				call, fn, ok := regexpCompileCall(n)
				if !ok {
					return true
				}
				iss := lintIssue{
					File:     f,
					Rule:     "regexp-compile-in-func",
					Severity: "warning",
					Message: fmt.Sprintf("regexp.%s with a constant pattern inside %s (line %d) — compiled on every call; hoist to a package-level var",
						fn, funcKey(fd), fset.Position(call.Pos()).Line),
				}
				if fn == "MustCompile" {
					iss.AutoFix = "hoist-regexp"
					iss.AutoFixCmd = fmt.Sprintf("hoist-regexp %s %s", f, fd.Name.Name)
				}
				out = append(out, iss)
				return true
			})
		}
	}
	return out
}

// AutoFix implements FixableRule via the hoist-regexp command.
func (r regexpHoistRule) AutoFix(issue lintIssue, _ LintContext) error {
	if issue.AutoFixCmd == "" {
		return fmt.Errorf("no autofix for this site (regexp.Compile returns an error; hoist manually)")
	}
	parts := strings.Fields(issue.AutoFixCmd)
	if len(parts) < 2 {
		return fmt.Errorf("invalid autofixCmd: %q", issue.AutoFixCmd)
	}
	return hoistRegexpCommand(parts[1:])
}

// regexpCompileCall matches regexp.MustCompile/regexp.Compile with a single
// string-literal pattern and returns the call and function name.
func regexpCompileCall(n ast.Node) (*ast.CallExpr, string, bool) {
	call, ok := n.(*ast.CallExpr)
	if !ok {
		return nil, "", false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || (sel.Sel.Name != "MustCompile" && sel.Sel.Name != "Compile") {
		return nil, "", false
	}
	if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "regexp" {
		return nil, "", false
	}
	if len(call.Args) != 1 {
		return nil, "", false
	}
	if lit, ok := call.Args[0].(*ast.BasicLit); !ok || lit.Kind != token.STRING {
		return nil, "", false
	}
	return call, sel.Sel.Name, true
}

// funcKey renders a function or Receiver:Method display name.
func funcKey(fd *ast.FuncDecl) string {
	if fd.Recv != nil && len(fd.Recv.List) > 0 {
		if recv := receiverTypeName(fd.Recv.List[0].Type); recv != "" {
			return recv + ":" + fd.Name.Name
		}
	}
	return fd.Name.Name
}
