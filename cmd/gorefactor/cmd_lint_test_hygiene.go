package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// Test-hygiene rules: the gate-integrity slice of the react-doctor-adapted
// inventory (docs/doctor-design-plan.md). Both target agent-written test
// smells that build+test cannot see — a vacuous test corrupts the very gate
// the harness rests on, and sleep-based synchronization makes it flaky.

// tbFailMethods are the testing.TB calls that can fail or end a test. A test
// invoking none of them — and never handing its *testing.T to a helper — can
// never fail.
var tbFailMethods = map[string]bool{
	"Error": true, "Errorf": true, "Fatal": true, "Fatalf": true,
	"Fail": true, "FailNow": true, "Skip": true, "Skipf": true, "SkipNow": true,
}

type vacuousTestRule struct{}

func (vacuousTestRule) Name() string { return "vacuous-test" }

func (r vacuousTestRule) Run(ctx LintContext) []lintIssue {
	var out []lintIssue
	for _, f := range ctx.Files {
		if !strings.HasSuffix(f, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		astFile, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			continue
		}
		for _, decl := range astFile.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil || !isTestFunc(fd) {
				continue
			}
			if testCanFail(fd) {
				continue
			}
			out = append(out, lintIssue{
				File:     f,
				Rule:     "vacuous-test",
				Severity: "warning",
				Message: fmt.Sprintf("%s cannot fail (line %d): no t.Error/t.Fatal/t.Skip and *testing.T is never passed to a helper — a green result is meaningless",
					fd.Name.Name, fset.Position(fd.Pos()).Line),
			})
		}
	}
	return out
}

// isTestFunc reports whether fd is a Test function taking *testing.T
// (TestMain, benchmarks, fuzz targets, and examples are out of scope).
func isTestFunc(fd *ast.FuncDecl) bool {
	name := fd.Name.Name
	if !strings.HasPrefix(name, "Test") || name == "TestMain" {
		return false
	}
	return len(fd.Type.Params.List) == 1 && testingTParamName(fd) != ""
}

// testingTParamName returns the name of fd's *testing.T parameter, or "".
func testingTParamName(fd *ast.FuncDecl) string {
	for _, p := range fd.Type.Params.List {
		star, ok := p.Type.(*ast.StarExpr)
		if !ok {
			continue
		}
		sel, ok := star.X.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "T" {
			continue
		}
		if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "testing" {
			continue
		}
		if len(p.Names) > 0 {
			return p.Names[0].Name
		}
	}
	return ""
}

// testCanFail reports whether the test body contains any path to failure:
// a TB fail/skip method call on any receiver (covers subtest closures and
// renamed params), a panic, or the *testing.T value passed as an argument to
// any call (a helper is assumed to assert). Deliberately under-reports:
// a logger.Error call also matches, and that is the right trade — a false
// "can fail" is noise-free, a false "vacuous" erodes trust in the rule.
func testCanFail(fd *ast.FuncDecl) bool {
	tb := testingTParamName(fd)
	canFail := false
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		if canFail {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fn := call.Fun.(type) {
		case *ast.SelectorExpr:
			if _, isIdent := fn.X.(*ast.Ident); isIdent && tbFailMethods[fn.Sel.Name] {
				canFail = true
				return false
			}
		case *ast.Ident:
			if fn.Name == "panic" {
				canFail = true
				return false
			}
		}
		for _, arg := range call.Args {
			if id, ok := arg.(*ast.Ident); ok && id.Name == tb {
				canFail = true // helper receives the *testing.T; assume it asserts
				return false
			}
		}
		return true
	})
	return canFail
}

type sleepInTestRule struct{}

func (sleepInTestRule) Name() string { return "sleep-in-test" }

func (r sleepInTestRule) Run(ctx LintContext) []lintIssue {
	var out []lintIssue
	for _, f := range ctx.Files {
		if !strings.HasSuffix(f, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		astFile, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			continue
		}
		ast.Inspect(astFile, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Sleep" {
				return true
			}
			if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "time" {
				return true
			}
			out = append(out, lintIssue{
				File:     f,
				Rule:     "sleep-in-test",
				Severity: "warning",
				Message: fmt.Sprintf("time.Sleep in test (line %d) — sleep-based synchronization is flaky; poll with a deadline, or use channels or a fake clock",
					fset.Position(call.Pos()).Line),
			})
			return true
		})
	}
	return out
}
