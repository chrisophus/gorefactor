package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
)

// pass-through-param is the prop-drilling analog from the react-doctor
// inventory (docs/doctor-design-plan.md, step 5): a parameter a function
// never uses itself, only forwards to another in-package function, chained
// deep enough that every signature in the middle exists solely to carry it.
// Name-based and package-local like the callgraph infra it builds on:
// receiver methods and cross-package calls are out of scope, and any
// shadowing of the parameter disqualifies it — the rule under-reports by
// design. Advisory (info): the fix is a restructuring judgment.
const passThroughDepthThreshold = 3

type passThroughParamRule struct{}

func (passThroughParamRule) Name() string { return "pass-through-param" }

func (r passThroughParamRule) Run(ctx LintContext) []lintIssue {
	var out []lintIssue
	for dir, files := range filesByDir(ctx.Files) {
		_ = dir
		out = append(out, passThroughIssuesForPackage(files)...)
	}
	return out
}

// filesByDir groups non-test files by directory (= package, near enough for
// a name-based rule).
func filesByDir(files []string) map[string][]string {
	out := map[string][]string{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		dir := filepath.Dir(f)
		out[dir] = append(out[dir], f)
	}
	return out
}

// pkgFunc is one top-level receiver-less function in the package index.
type pkgFunc struct {
	file string
	fset *token.FileSet
	decl *ast.FuncDecl
}

// forwardEdge records "this param is passed on to callee's param".
type forwardEdge struct {
	callee string
	param  string
}

func passThroughIssuesForPackage(files []string) []lintIssue {
	funcs := indexPackageFuncs(files)
	forwards := map[string]map[string][]forwardEdge{}
	for name, pf := range funcs {
		forwards[name] = forwardOnlyParams(pf, funcs)
	}
	var out []lintIssue
	for name, pf := range funcs {
		for _, p := range declParamNames(pf.decl) {
			depth := forwardDepth(name, p, forwards, map[string]bool{})
			if depth < passThroughDepthThreshold {
				continue
			}
			out = append(out, lintIssue{
				File:     pf.file,
				Rule:     "pass-through-param",
				Severity: "info",
				Message: fmt.Sprintf("parameter %q of %s (line %d) is forwarded through %d call layers without being used — prop drilling; consider a params struct or restructuring the call chain",
					p, name, pf.fset.Position(pf.decl.Pos()).Line, depth),
			})
		}
	}
	return out
}

// indexPackageFuncs maps function name to declaration for top-level
// receiver-less functions; duplicate names (build tags) are dropped.
func indexPackageFuncs(files []string) map[string]pkgFunc {
	funcs := map[string]pkgFunc{}
	dupes := map[string]bool{}
	for _, f := range files {
		fset := token.NewFileSet()
		astFile, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			continue
		}
		for _, decl := range astFile.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Recv != nil || fd.Body == nil {
				continue
			}
			if _, seen := funcs[fd.Name.Name]; seen {
				dupes[fd.Name.Name] = true
				continue
			}
			funcs[fd.Name.Name] = pkgFunc{file: f, fset: fset, decl: fd}
		}
	}
	for name := range dupes {
		delete(funcs, name)
	}
	return funcs
}

func declParamNames(fd *ast.FuncDecl) []string {
	var out []string
	for _, p := range fd.Type.Params.List {
		for _, n := range p.Names {
			if n.Name != "_" {
				out = append(out, n.Name)
			}
		}
	}
	return out
}

// forwardOnlyParams returns, for each parameter of pf that is never used
// directly, the in-package calls it is forwarded to.
func forwardOnlyParams(pf pkgFunc, funcs map[string]pkgFunc) map[string][]forwardEdge {
	out := map[string][]forwardEdge{}
	for _, p := range declParamNames(pf.decl) {
		if paramShadowed(pf.decl.Body, p) {
			continue
		}
		edges, realUse := classifyParamUses(pf.decl.Body, p, funcs)
		if !realUse && len(edges) > 0 {
			out[p] = edges
		}
	}
	return out
}

// classifyParamUses splits uses of param p into forwards (bare argument to an
// indexed in-package function) and real uses (anything else).
func classifyParamUses(body *ast.BlockStmt, p string, funcs map[string]pkgFunc) (edges []forwardEdge, realUse bool) {
	forwardedAt := map[token.Pos]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		fn, ok := call.Fun.(*ast.Ident)
		if !ok {
			return true
		}
		callee, indexed := funcs[fn.Name]
		if !indexed {
			return true
		}
		calleeParams := declParamNames(callee.decl)
		if len(calleeParams) != len(call.Args) {
			return true // variadic or multi-name mismatch: stay conservative
		}
		for i, arg := range call.Args {
			if id, ok := arg.(*ast.Ident); ok && id.Name == p {
				forwardedAt[id.Pos()] = true
				edges = append(edges, forwardEdge{callee: fn.Name, param: calleeParams[i]})
			}
		}
		return true
	})
	ast.Inspect(body, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Name == p && !forwardedAt[id.Pos()] {
			realUse = true
		}
		return !realUse
	})
	return edges, realUse
}

// paramShadowed reports whether p is redeclared anywhere in the body — the
// bail-out that keeps the name-based analysis honest.
func paramShadowed(body *ast.BlockStmt, p string) bool {
	shadowed := false
	ast.Inspect(body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.AssignStmt:
			if node.Tok == token.DEFINE {
				for _, lhs := range node.Lhs {
					if id, ok := lhs.(*ast.Ident); ok && id.Name == p {
						shadowed = true
					}
				}
			}
		case *ast.ValueSpec:
			for _, id := range node.Names {
				if id.Name == p {
					shadowed = true
				}
			}
		case *ast.FuncLit:
			for _, f := range node.Type.Params.List {
				for _, id := range f.Names {
					if id.Name == p {
						shadowed = true
					}
				}
			}
		case *ast.RangeStmt:
			for _, e := range []ast.Expr{node.Key, node.Value} {
				if id, ok := e.(*ast.Ident); ok && id.Name == p {
					shadowed = true
				}
			}
		}
		return !shadowed
	})
	return shadowed
}

// forwardDepth counts consecutive forward-only functions starting at (fn, p).
// A function that actually uses the parameter contributes 0.
func forwardDepth(fn, p string, forwards map[string]map[string][]forwardEdge, visiting map[string]bool) int {
	key := fn + ":" + p
	if visiting[key] {
		return 0 // recursion: treat the cycle as use
	}
	edges, ok := forwards[fn][p]
	if !ok {
		return 0
	}
	visiting[key] = true
	defer delete(visiting, key)
	max := 0
	for _, e := range edges {
		if d := forwardDepth(e.callee, e.param, forwards, visiting); d > max {
			max = d
		}
	}
	return 1 + max
}
