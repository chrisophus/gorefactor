package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
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

func hasEntryNilGuard(fd *ast.FuncDecl, param string) bool {
	if fd.Body == nil || len(fd.Body.List) == 0 {
		return false
	}
	for _, stmt := range fd.Body.List {
		switch s := stmt.(type) {
		case *ast.AssignStmt, *ast.DeclStmt, *ast.ExprStmt:
			continue
		case *ast.IfStmt:
			return isNilCompareOp(s.Cond, param, token.EQL) && bodyStartsWithReturn(s.Body)
		default:
			return false
		}
	}
	return false
}

func isNilCompareOp(cond ast.Expr, param string, op token.Token) bool {
	bin, ok := cond.(*ast.BinaryExpr)
	if !ok || bin.Op != op {
		return false
	}
	return (isIdentName(bin.X, param) && isNilIdent(bin.Y)) ||
		(isIdentName(bin.Y, param) && isNilIdent(bin.X))
}

func isIdentName(e ast.Expr, name string) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == name
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

func argProvenNonNil(fn *ast.FuncDecl, call *ast.CallExpr, arg ast.Expr) bool {
	if exprConstructsNonNil(arg) {
		return true
	}
	id, ok := arg.(*ast.Ident)
	if !ok {
		return false
	}
	if localAssignedNonNil(fn.Body, call, id.Name) {
		return true
	}
	if enclosingNonNilGuard(fn.Body, call, id.Name) {
		return true
	}
	return precedingNilReject(fn.Body, call, id.Name)
}

func exprConstructsNonNil(arg ast.Expr) bool {
	switch a := arg.(type) {
	case *ast.UnaryExpr:
		return a.Op == token.AND
	case *ast.CallExpr:
		if id, ok := a.Fun.(*ast.Ident); ok && id.Name == "new" {
			return true
		}
	}
	return false
}

func localAssignedNonNil(body *ast.BlockStmt, call *ast.CallExpr, name string) bool {
	// Walk statement lists in order. On each assign/decl of name, record whether
	// the RHS constructs a non-nil value. Stop at the statement that contains
	// call (after scanning earlier siblings in nested blocks that contain call).
	var lastNonNil bool
	var seen bool
	var walk func([]ast.Stmt) bool // returns true when call was reached
	walk = func(list []ast.Stmt) bool {
		for _, stmt := range list {
			if nodeContains(stmt, call) {
				switch s := stmt.(type) {
				case *ast.IfStmt:
					if walk(s.Body.List) {
						return true
					}
					if s.Else != nil {
						if b, ok := s.Else.(*ast.BlockStmt); ok && walk(b.List) {
							return true
						}
					}
				case *ast.BlockStmt:
					return walk(s.List)
				case *ast.ForStmt:
					return walk(s.Body.List)
				case *ast.RangeStmt:
					return walk(s.Body.List)
				}
				return true
			}
			recordAssign := func(names []*ast.Ident, values []ast.Expr) {
				for i, n := range names {
					if n == nil || n.Name != name {
						continue
					}
					seen = true
					if i < len(values) && exprConstructsNonNil(values[i]) {
						lastNonNil = true
					} else {
						lastNonNil = false
					}
				}
			}
			switch s := stmt.(type) {
			case *ast.AssignStmt:
				lhs := make([]*ast.Ident, len(s.Lhs))
				for i, e := range s.Lhs {
					lhs[i], _ = e.(*ast.Ident)
				}
				recordAssign(lhs, s.Rhs)
			case *ast.DeclStmt:
				gen, ok := s.Decl.(*ast.GenDecl)
				if !ok {
					continue
				}
				for _, spec := range gen.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					recordAssign(vs.Names, vs.Values)
				}
			}
		}
		return false
	}
	walk(body.List)
	return seen && lastNonNil
}

func enclosingNonNilGuard(body *ast.BlockStmt, call *ast.CallExpr, name string) bool {
	var found bool
	ast.Inspect(body, func(n ast.Node) bool {
		if found || n == nil {
			return false
		}
		ifs, ok := n.(*ast.IfStmt)
		if !ok {
			return true
		}
		if isNilCompareOp(ifs.Cond, name, token.NEQ) && nodeContains(ifs.Body, call) {
			found = true
			return false
		}
		return true
	})
	return found
}

func precedingNilReject(body *ast.BlockStmt, call *ast.CallExpr, name string) bool {
	var okProven bool
	var walkBlocks func(list []ast.Stmt)
	walkBlocks = func(list []ast.Stmt) {
		if okProven {
			return
		}
		for i, stmt := range list {
			if nodeContains(stmt, call) {
				for j := 0; j < i; j++ {
					ifs, ok := list[j].(*ast.IfStmt)
					if ok && isNilCompareOp(ifs.Cond, name, token.EQL) && bodyStartsWithReturn(ifs.Body) {
						okProven = true
						return
					}
				}
			}
			ast.Inspect(stmt, func(n ast.Node) bool {
				if b, ok := n.(*ast.BlockStmt); ok && n != stmt {
					walkBlocks(b.List)
					return false
				}
				return !okProven
			})
		}
	}
	walkBlocks(body.List)
	return okProven
}

func isUnexportedName(name string) bool {
	if name == "" {
		return false
	}
	r, _ := utf8.DecodeRuneInString(name)
	return unicode.IsLower(r)
}
