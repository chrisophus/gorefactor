package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strings"
)

// Concurrency rules from the react-doctor-adapted inventory
// (docs/doctor-design-plan.md, step 5) — the effect-needs-cleanup family:
// resources and goroutines launched in library code without a visible
// lifecycle. Shape-conditioned like fatal-in-library: package main owns its
// process lifetime and is exempt.

type unstoppedTickerRule struct{}

func (unstoppedTickerRule) Name() string { return "unstopped-ticker" }

// Run flags time.NewTicker results whose Stop is never called in the same
// function. Tickers leak their goroutine until stopped; a ticker that
// outlives the function on purpose should be stopped by the owner it is
// handed to — returning it unexported with no Stop anywhere is the bug.
func (r unstoppedTickerRule) Run(ctx LintContext) []lintIssue {
	var out []lintIssue
	forEachLibraryFunc(ctx, func(f string, fset *token.FileSet, fd *ast.FuncDecl) {
		for _, tick := range unstoppedTickers(fd) {
			out = append(out, lintIssue{
				File:     f,
				Rule:     "unstopped-ticker",
				Severity: "warning",
				Message: fmt.Sprintf("%s: time.NewTicker at line %d is never stopped — its goroutine leaks; defer %s.Stop() or hand ownership to a caller that stops it",
					fd.Name.Name, fset.Position(tick.pos).Line, tick.name),
			})
		}
	})
	return out
}

type tickerSite struct {
	name string
	pos  token.Pos
}

// unstoppedTickers returns tickers assigned in fd whose .Stop() never appears
// in the body. A ticker returned from the function is exempt: ownership
// transferred.
func unstoppedTickers(fd *ast.FuncDecl) []tickerSite {
	var sites []tickerSite
	stopped := map[string]bool{}
	returned := map[string]bool{}
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.AssignStmt:
			for i, rhs := range node.Rhs {
				if !isTimeCall(rhs, "NewTicker") || i >= len(node.Lhs) {
					continue
				}
				if id, ok := node.Lhs[i].(*ast.Ident); ok && id.Name != "_" {
					sites = append(sites, tickerSite{name: id.Name, pos: rhs.Pos()})
				}
			}
		case *ast.CallExpr:
			if sel, ok := node.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Stop" {
				if id, ok := sel.X.(*ast.Ident); ok {
					stopped[id.Name] = true
				}
			}
		case *ast.ReturnStmt:
			for _, res := range node.Results {
				if id, ok := res.(*ast.Ident); ok {
					returned[id.Name] = true
				}
			}
		}
		return true
	})
	var leaks []tickerSite
	for _, s := range sites {
		if !stopped[s.name] && !returned[s.name] {
			leaks = append(leaks, s)
		}
	}
	return leaks
}

func isTimeCall(e ast.Expr, name string) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != name {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "time"
}

type nakedGoroutineRule struct{}

func (nakedGoroutineRule) Name() string { return "naked-goroutine" }

// lifecycleIdent matches identifiers that signal the goroutine participates
// in a lifecycle: contexts, done/quit channels, wait groups, semaphores.
var lifecycleIdent = regexp.MustCompile(`(?i)^(ctx|context|done|quit|stop|cancel|wg|waitgroup|group|g|eg|errgroup|sem|ch)$`)

// Run flags `go` statements in library code with no visible lifecycle signal
// — nothing to cancel it, wait for it, or observe its exit. Advisory (info):
// the heuristic is name-based and fire-and-forget goroutines are sometimes
// intentional; the finding is a prompt to make the lifecycle explicit.
func (r nakedGoroutineRule) Run(ctx LintContext) []lintIssue {
	var out []lintIssue
	forEachLibraryFunc(ctx, func(f string, fset *token.FileSet, fd *ast.FuncDecl) {
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			goStmt, ok := n.(*ast.GoStmt)
			if !ok || goroutineHasLifecycle(goStmt) {
				return true
			}
			out = append(out, lintIssue{
				File:     f,
				Rule:     "naked-goroutine",
				Severity: "info",
				Message: fmt.Sprintf("%s: goroutine at line %d has no visible lifecycle — no context, done channel, or WaitGroup; callers cannot cancel it or know it finished",
					fd.Name.Name, fset.Position(goStmt.Pos()).Line),
			})
			return true
		})
	})
	return out
}

// goroutineHasLifecycle reports whether the go statement references any
// lifecycle-signaling identifier in its call or function-literal body.
func goroutineHasLifecycle(goStmt *ast.GoStmt) bool {
	found := false
	ast.Inspect(goStmt.Call, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.Ident:
			if lifecycleIdent.MatchString(node.Name) {
				found = true
			}
		case *ast.SelectorExpr:
			// wg.Done(), g.Go(), ctx.Done() — selector receivers count too.
			if id, ok := node.X.(*ast.Ident); ok && lifecycleIdent.MatchString(id.Name) {
				found = true
			}
		}
		return !found
	})
	return found
}

// forEachLibraryFunc invokes fn for every function declaration in non-main,
// non-test files — the shared shape condition for the conc rules.
func forEachLibraryFunc(ctx LintContext, fn func(file string, fset *token.FileSet, fd *ast.FuncDecl)) {
	for _, f := range ctx.Files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		astFile, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil || astFile.Name.Name == "main" {
			continue
		}
		for _, decl := range astFile.Decls {
			if fd, ok := decl.(*ast.FuncDecl); ok && fd.Body != nil {
				fn(f, fset, fd)
			}
		}
	}
}
