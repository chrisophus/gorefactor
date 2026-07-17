package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// Perf rules from the react-doctor-adapted inventory
// (docs/doctor-design-plan.md, step 5): allocation patterns that compile and
// test green but degrade quietly — the string-concat and repeated-lookup
// analogs of react-doctor's hot-path rules. Parse-only heuristics that
// deliberately under-report: without type information, only provably-string
// concatenation and structurally-obvious scans are flagged.

type stringConcatInLoopRule struct{}

func (stringConcatInLoopRule) Name() string { return "string-concat-in-loop" }

func (r stringConcatInLoopRule) Run(ctx LintContext) []lintIssue {
	var out []lintIssue
	seen := map[string]bool{}
	forEachLoopBody(ctx, func(f string, fset *token.FileSet, loop ast.Node, body *ast.BlockStmt) {
		declaredInLoop := identsDeclaredIn(body)
		ast.Inspect(body, func(n ast.Node) bool {
			assign, ok := n.(*ast.AssignStmt)
			if !ok || !isStringConcatAssign(assign) {
				return true
			}
			// An accumulator declared inside the loop is reset every
			// iteration, so the concatenation is bounded — not quadratic.
			// Only a variable declared outside the loop grows across
			// iterations, which is the O(n²) pattern worth flagging.
			if lhs, ok := assign.Lhs[0].(*ast.Ident); ok && declaredInLoop[lhs.Name] {
				return true
			}
			line := fset.Position(n.Pos()).Line
			key := fmt.Sprintf("%s:%d", f, line)
			if seen[key] {
				return true
			}
			seen[key] = true
			out = append(out, lintIssue{
				File:     f,
				Rule:     "string-concat-in-loop",
				Severity: "warning",
				Message:  fmt.Sprintf("string concatenation in loop (line %d) — quadratic allocation; use strings.Builder", line),
			})
			return true
		})
	})
	return out
}

// identsDeclaredIn collects the names of variables declared within body's
// subtree via `:=`, `var`, or as range loop variables.
func identsDeclaredIn(body *ast.BlockStmt) map[string]bool {
	declared := map[string]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		switch d := n.(type) {
		case *ast.AssignStmt:
			if d.Tok == token.DEFINE {
				for _, lhs := range d.Lhs {
					if id, ok := lhs.(*ast.Ident); ok {
						declared[id.Name] = true
					}
				}
			}
		case *ast.RangeStmt:
			for _, v := range []ast.Expr{d.Key, d.Value} {
				if id, ok := v.(*ast.Ident); ok {
					declared[id.Name] = true
				}
			}
		case *ast.ValueSpec:
			for _, id := range d.Names {
				declared[id.Name] = true
			}
		}
		return true
	})
	return declared
}

// isStringConcatAssign reports whether assign grows a string: `s += <string>`
// or `s = s + <string>` where the added value is provably a string
// (a string literal or fmt.Sprintf call somewhere in the chain).
func isStringConcatAssign(assign *ast.AssignStmt) bool {
	if len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
		return false
	}
	lhs, ok := assign.Lhs[0].(*ast.Ident)
	if !ok {
		return false
	}
	switch assign.Tok {
	case token.ADD_ASSIGN:
		return isStringExpr(assign.Rhs[0])
	case token.ASSIGN:
		bin, ok := assign.Rhs[0].(*ast.BinaryExpr)
		if !ok || bin.Op != token.ADD {
			return false
		}
		left, ok := bin.X.(*ast.Ident)
		if !ok || left.Name != lhs.Name {
			return false
		}
		return isStringExpr(bin.Y) || isStringExpr(bin.X)
	}
	return false
}

// isStringExpr reports whether e is provably a string expression: a string
// literal, an fmt.Sprintf call, or a concatenation involving one.
func isStringExpr(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.BasicLit:
		return v.Kind == token.STRING
	case *ast.BinaryExpr:
		return v.Op == token.ADD && (isStringExpr(v.X) || isStringExpr(v.Y))
	case *ast.CallExpr:
		sel, ok := v.Fun.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		pkg, ok := sel.X.(*ast.Ident)
		return ok && pkg.Name == "fmt" && strings.HasPrefix(sel.Sel.Name, "Sprint")
	}
	return false
}

type linearSearchInLoopRule struct{}

func (linearSearchInLoopRule) Name() string { return "linear-search-in-loop" }

// Run flags equality scans nested inside another loop. Info severity: the rule cannot see n, and
// observation on this repo showed most hits are scans over small constant slices where a map would
// be overkill — the finding is a prompt, not a defect.
func (r linearSearchInLoopRule) Run(ctx LintContext) []lintIssue {
	return scanLoopsDeduped(ctx, func(n ast.Node) (string, string, bool) {
		inner, ok := n.(*ast.RangeStmt)
		if !ok || !isLinearScan(inner) {
			return "", "", false
		}
		return "linear-search-in-loop", "linear search inside a loop (line %d) — repeated O(n) scans; build a map keyed by the compared value before the outer loop", true
	})

}

// isLinearScan reports whether rng is a pure equality scan: a range loop
// whose body is a single if with an == comparison referencing the loop
// variable. That shape is a lookup — the map-rewrite candidate.
func isLinearScan(rng *ast.RangeStmt) bool {
	if len(rng.Body.List) != 1 {
		return false
	}
	ifStmt, ok := rng.Body.List[0].(*ast.IfStmt)
	if !ok || ifStmt.Init != nil {
		return false
	}
	cond, ok := ifStmt.Cond.(*ast.BinaryExpr)
	if !ok || cond.Op != token.EQL {
		return false
	}
	loopVar := ""
	if v, ok := rng.Value.(*ast.Ident); ok && v.Name != "_" {
		loopVar = v.Name
	} else if k, ok := rng.Key.(*ast.Ident); ok && k.Name != "_" {
		loopVar = k.Name
	}
	return loopVar != "" && (exprReferences(cond.X, loopVar) || exprReferences(cond.Y, loopVar))
}

// exprReferences reports whether e mentions the identifier name.
func exprReferences(e ast.Expr, name string) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Name == name {
			found = true
		}
		return !found
	})
	return found
}

// forEachLoopBody parses each non-test file in ctx and invokes fn for every
// for/range loop body — the shared walk for the loop-shaped perf rules.
func forEachLoopBody(ctx LintContext, fn func(file string, fset *token.FileSet, loop ast.Node, body *ast.BlockStmt)) {
	for _, f := range ctx.Files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		astFile, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			continue
		}
		ast.Inspect(astFile, func(n ast.Node) bool {
			switch loop := n.(type) {
			case *ast.ForStmt:
				fn(f, fset, loop, loop.Body)
			case *ast.RangeStmt:
				fn(f, fset, loop, loop.Body)
			}
			return true
		})
	}
}

func scanLoopsDeduped(ctx LintContext, match func(ast.Node) (rule, msgFmt string, ok bool)) []lintIssue {
	var out []lintIssue
	seen := map[string]bool{}
	forEachLoopBody(ctx, func(f string, fset *token.FileSet, loop ast.Node, body *ast.BlockStmt) {
		ast.Inspect(body, func(n ast.Node) bool {
			rule, msgFmt, ok := match(n)
			if !ok {
				return true
			}
			line := fset.Position(n.Pos()).Line
			key := fmt.Sprintf("%s:%s:%d", rule, f, line)
			if seen[key] {
				return true
			}
			seen[key] = true
			severity := "warning"
			if rule == "linear-search-in-loop" {
				severity = "info"
			}
			out = append(out, lintIssue{
				File:     f,
				Rule:     rule,
				Severity: severity,
				Message:  fmt.Sprintf(msgFmt, line),
			})
			return true
		})
	})
	return out
}
