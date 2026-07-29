package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// redundant-nil-guard flags unexported functions whose pointer (or interface)
// parameters are nil-checked at entry even though every in-package caller
// already treats those arguments as non-nil invariants. Exported APIs are out
// of scope — callers outside the package may pass nil. The rule under-reports
// by design: any caller that does not clearly establish the invariant
// suppresses the finding.
//
// Scope (v1): package-local plain functions only (no methods — receiver
// arguments are not in CallExpr.Args and need type info to resolve safely).
// A function's entry prologue may hold several guards in sequence, and a
// combined `a == nil || b == nil` reject counts as a guard for each name;
// caller-side proofs likewise accept `x != nil && …` and `x == nil || y ==
// nil` conditions.

type redundantNilGuardRule struct{}

func (redundantNilGuardRule) Name() string { return "redundant-nil-guard" }

func (r redundantNilGuardRule) Run(ctx LintContext) []lintIssue {
	var out []lintIssue
	for _, files := range filesByDir(ctx.Files) {
		out = append(out, redundantNilGuardIssues(files)...)
	}
	return out
}

type nilGuardFunc struct {
	file   string
	fset   *token.FileSet
	decl   *ast.FuncDecl
	params []nilGuardParam
}

type nilGuardParam struct {
	name  string
	kind  string // "pointer" or "interface"
	index int    // position in CallExpr.Args
}

func redundantNilGuardIssues(files []string) []lintIssue {
	funcs, calls := indexNilGuardPackage(files)
	var out []lintIssue
	for name, fn := range funcs {
		if !isUnexportedName(fn.decl.Name.Name) {
			continue
		}
		callSites := calls[name]
		if len(callSites) == 0 {
			continue
		}
		for _, p := range fn.params {
			if !hasEntryNilGuard(fn.decl, p.name) {
				continue
			}
			if !allCallersProveNonNil(callSites, p.index) {
				continue
			}
			out = append(out, lintIssue{
				File:     fn.file,
				Rule:     "redundant-nil-guard",
				Severity: "warning",
				Message: fmt.Sprintf("%s nil-checks parameter %q (%s) at entry (line %d) but all %d in-package caller(s) already establish it as non-nil — drop the guard or document why nil remains reachable",
					name, p.name, p.kind, fn.fset.Position(fn.decl.Pos()).Line, len(callSites)),
			})
		}
	}
	return out
}

type nilCallSite struct {
	fn   *ast.FuncDecl
	call *ast.CallExpr
	args []ast.Expr
}

func indexNilGuardPackage(files []string) (map[string]*nilGuardFunc, map[string][]nilCallSite) {
	funcs := map[string]*nilGuardFunc{}
	var parsed []struct {
		file string
		fset *token.FileSet
		ast  *ast.File
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		astFile, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			continue
		}
		parsed = append(parsed, struct {
			file string
			fset *token.FileSet
			ast  *ast.File
		}{f, fset, astFile})
		for _, decl := range astFile.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil || fd.Name == nil || fd.Recv != nil {
				continue // methods out of scope for v1
			}
			funcs[fd.Name.Name] = &nilGuardFunc{
				file:   f,
				fset:   fset,
				decl:   fd,
				params: nilableParams(fd),
			}
		}
	}
	calls := map[string][]nilCallSite{}
	for _, p := range parsed {
		for _, decl := range p.ast.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				id, ok := call.Fun.(*ast.Ident)
				if !ok {
					return true
				}
				if _, ok := funcs[id.Name]; !ok {
					return true
				}
				calls[id.Name] = append(calls[id.Name], nilCallSite{
					fn:   fd,
					call: call,
					args: append([]ast.Expr(nil), call.Args...),
				})
				return true
			})
		}
	}
	return funcs, calls
}

func nilableParams(fd *ast.FuncDecl) []nilGuardParam {
	var out []nilGuardParam
	if fd.Type.Params == nil {
		return out
	}
	argIndex := 0
	for _, f := range fd.Type.Params.List {
		kind := nilableKind(f.Type)
		names := f.Names
		if len(names) == 0 {
			argIndex++
			continue
		}
		for _, n := range names {
			if kind != "" && n != nil && n.Name != "_" && n.Name != "" {
				out = append(out, nilGuardParam{name: n.Name, kind: kind, index: argIndex})
			}
			argIndex++
		}
	}
	return out
}

func nilableKind(typ ast.Expr) string {
	switch t := typ.(type) {
	case *ast.StarExpr:
		return "pointer"
	case *ast.InterfaceType:
		return "interface"
	case *ast.Ident:
		switch t.Name {
		case "any", "error":
			return "interface"
		}
	}
	return ""
}

// hasEntryNilGuard reports whether fd nil-checks param in its entry prologue.
// The prologue may hold several guards in sequence — each an `x == nil`-shaped
// reject (a single compare or an `||` chain of them) whose body immediately
// returns — interleaved with simple assign/decl/expr statements. Scanning
// continues past guards for other parameters, so every guard in the prologue
// is recognized, not just the first.
func hasEntryNilGuard(fd *ast.FuncDecl, param string) bool {
	if fd.Body == nil || len(fd.Body.List) == 0 {
		return false
	}
	for _, stmt := range fd.Body.List {
		switch s := stmt.(type) {
		case *ast.AssignStmt, *ast.DeclStmt, *ast.ExprStmt:
			continue
		case *ast.IfStmt:
			if !bodyStartsWithReturn(s.Body) {
				return false
			}
			names := nilRejectGuardNames(s.Cond)
			if len(names) == 0 {
				return false
			}
			if slices.Contains(names, param) {
				return true
			}
			continue // a nil guard for other params — still function entry
		default:
			return false
		}
	}
	return false
}

// nilRejectGuardNames returns the names x for which cond is an `x == nil`
// test, treating a top-level `||` chain as one combined guard: falling past
// `if a == nil || b == nil { return }` proves both a and b non-nil. Any
// disjunct that is not a plain `ident == nil` compare disqualifies the whole
// condition — a mixed guard does more than nil-check, so "drop the guard" is
// not safe advice for it.
func nilRejectGuardNames(cond ast.Expr) []string {
	return nilEqDisjunctNames(cond, true)
}

// nilEqDisjunctNames collects the names x with an `x == nil` disjunct at the
// top level of cond. With strict set, every disjunct must be such a compare
// or nothing is returned; loose callers rely only on "cond false ⇒ every
// disjunct false", which tolerates mixed conditions like
// `err != nil || x == nil`.
func nilEqDisjunctNames(cond ast.Expr, strict bool) []string {
	var names []string
	for _, d := range flattenBool(cond, token.LOR) {
		if be, ok := stripParens(d).(*ast.BinaryExpr); ok {
			if name, ok := identNilCompare(be, token.EQL); ok {
				names = append(names, name)
				continue
			}
		}
		if strict {
			return nil
		}
	}
	return names
}

// nonNilConjunctNames returns the names x for which cond being true
// establishes x != nil: the top-level `&&` conjuncts of cond that are plain
// `x != nil` compares. Other conjuncts are allowed — a true conjunction makes
// every conjunct true.
func nonNilConjunctNames(cond ast.Expr) []string {
	var names []string
	for _, c := range flattenBool(cond, token.LAND) {
		if be, ok := stripParens(c).(*ast.BinaryExpr); ok {
			if name, ok := identNilCompare(be, token.NEQ); ok {
				names = append(names, name)
			}
		}
	}
	return names
}

// flattenBool splits e into its leaves under the given boolean operator
// (token.LAND or token.LOR), looking through parentheses.
func flattenBool(e ast.Expr, op token.Token) []ast.Expr {
	if be, ok := stripParens(e).(*ast.BinaryExpr); ok && be.Op == op {
		return append(flattenBool(be.X, op), flattenBool(be.Y, op)...)
	}
	return []ast.Expr{e}
}

// identNilCompare returns the identifier compared against nil when be is a
// plain `ident <op> nil` (or `nil <op> ident`) comparison.
func identNilCompare(be *ast.BinaryExpr, op token.Token) (string, bool) {
	if be.Op != op {
		return "", false
	}
	other := ast.Expr(nil)
	if isNilIdent(be.Y) {
		other = be.X
	} else if isNilIdent(be.X) {
		other = be.Y
	} else {
		return "", false
	}
	id, ok := stripParens(other).(*ast.Ident)
	if !ok || id.Name == "_" || id.Name == "nil" {
		return "", false
	}
	return id.Name, true
}

func stripParens(e ast.Expr) ast.Expr {
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			return e
		}
		e = p.X
	}
}

func isNilIdent(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == "nil"
}

func bodyStartsWithReturn(body *ast.BlockStmt) bool {
	if body == nil || len(body.List) == 0 {
		return false
	}
	_, ok := body.List[0].(*ast.ReturnStmt)
	return ok
}

func allCallersProveNonNil(sites []nilCallSite, argIndex int) bool {
	for _, site := range sites {
		if argIndex >= len(site.args) {
			return false
		}
		if !argProvenNonNil(site.fn, site.call, site.args[argIndex]) {
			return false
		}
	}
	return true
}

func isUnexportedName(name string) bool {
	if name == "" {
		return false
	}
	r, _ := utf8.DecodeRuneInString(name)
	return unicode.IsLower(r)
}
