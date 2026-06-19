package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

// sigCallSite is one rewritable call of the target function/method.
type sigCallSite struct {
	pkg  *packages.Package
	file string
	call *ast.CallExpr
}

// calleeIdent returns the identifier a call resolves through (plain call or
// selector call), or nil for indirect calls.
func calleeIdent(call *ast.CallExpr) *ast.Ident {
	switch f := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		return f
	case *ast.SelectorExpr:
		return f.Sel
	}
	return nil
}

// gatherFuncRefs finds every reference to the declaration at declPos across
// all loaded packages (including _test.go variants). References that are not
// direct calls (function used as a value, stored, passed, etc.) come back as
// badRefs — those make a signature change unsafe.
func gatherFuncRefs(pkgs []*packages.Package, declPos token.Pos) (sites []sigCallSite, badRefs []string) {
	seenSite := map[string]bool{}
	seenBad := map[string]bool{}
	for _, p := range pkgs {
		if p.TypesInfo == nil {
			continue
		}
		for _, f := range p.Syntax {
			callFor := map[token.Pos]*ast.CallExpr{}
			ast.Inspect(f, func(n ast.Node) bool {
				if call, ok := n.(*ast.CallExpr); ok {
					if id := calleeIdent(call); id != nil {
						callFor[id.Pos()] = call
					}
				}
				return true
			})
			ast.Inspect(f, func(n ast.Node) bool {
				id, ok := n.(*ast.Ident)
				if !ok {
					return true
				}
				obj := p.TypesInfo.Uses[id]
				if obj == nil || obj.Pos() != declPos {
					return true
				}
				pos := p.Fset.Position(id.Pos())
				if call := callFor[id.Pos()]; call != nil {
					key := fmt.Sprintf("%s:%d", pos.Filename, pos.Offset)
					if !seenSite[key] {
						seenSite[key] = true
						sites = append(sites, sigCallSite{pkg: p, file: pos.Filename, call: call})
					}
				} else {
					key := fmt.Sprintf("%s:%d", pos.Filename, pos.Line)
					if !seenBad[key] {
						seenBad[key] = true
						badRefs = append(badRefs, key)
					}
				}
				return true
			})
		}
	}
	sort.Strings(badRefs)
	return sites, badRefs
}

// siteProblem reports why a call site cannot be rewritten mechanically
// ("" = safe). arity counts flattened parameters; variadicIndex is -1 for
// non-variadic targets.
func siteProblem(s sigCallSite, arity, variadicIndex int) string {
	info := s.pkg.TypesInfo
	if sel, ok := ast.Unparen(s.call.Fun).(*ast.SelectorExpr); ok {
		if tv, ok := info.Types[sel.X]; ok && tv.IsType() {
			return "method expression call (receiver passed as first argument)"
		}
	}
	for _, a := range s.call.Args {
		if tv, ok := info.Types[a]; ok {
			if _, isTuple := tv.Type.(*types.Tuple); isTuple {
				return "argument is a multi-value call expression"
			}
		}
	}
	n := len(s.call.Args)
	if variadicIndex < 0 {
		if n != arity {
			return fmt.Sprintf("argument count %d does not match parameter count %d", n, arity)
		}
		return ""
	}
	if s.call.Ellipsis.IsValid() {
		if n != arity {
			return "spread (...) call with unexpected argument count"
		}
		return ""
	}
	if n < variadicIndex {
		return fmt.Sprintf("argument count %d below required %d", n, variadicIndex)
	}
	return ""
}

// checkRewriteSafety bundles the two global refusal conditions: non-call
// references and per-site rewrite problems. Returns an exit-2 error listing
// every offending site, or nil when all call sites are mechanically safe.
func checkRewriteSafety(locator string, badRefs []string, sites []sigCallSite, arity, variadicIndex int) error {
	var problems []string
	for _, r := range badRefs {
		problems = append(problems, r+"  (function used as a value, not called)")
	}
	for _, s := range sites {
		if reason := siteProblem(s, arity, variadicIndex); reason != "" {
			problems = append(problems, s.location()+"  ("+reason+")")
		}
	}
	if len(problems) == 0 {
		return nil
	}
	sort.Strings(problems)
	return notFoundErrorf(
		"cannot safely rewrite all uses of %s; fix these sites first or update them manually:\n  %s",
		locator, strings.Join(problems, "\n  "))
}

// insertArgEdit inserts value as argument idx of the call.
func insertArgEdit(s sigCallSite, idx int, value string) textEdit {
	fset := s.pkg.Fset
	args := s.call.Args
	if len(args) == 0 {
		off := fset.Position(s.call.Lparen).Offset + 1
		return textEdit{file: s.file, start: off, end: off, text: value}
	}
	if idx >= len(args) {
		off := fset.Position(args[len(args)-1].End()).Offset
		return textEdit{file: s.file, start: off, end: off, text: ", " + value}
	}
	off := fset.Position(args[idx].Pos()).Offset
	return textEdit{file: s.file, start: off, end: off, text: value + ", "}
}

// removeArgEdit drops argument idx (including its separating comma).
func removeArgEdit(s sigCallSite, idx int) textEdit {
	fset := s.pkg.Fset
	args := s.call.Args
	if len(args) == 1 {
		return textEdit{file: s.file,
			start: fset.Position(args[0].Pos()).Offset,
			end:   fset.Position(args[0].End()).Offset}
	}
	if idx == len(args)-1 {
		return textEdit{file: s.file,
			start: fset.Position(args[idx-1].End()).Offset,
			end:   fset.Position(args[idx].End()).Offset}
	}
	return textEdit{file: s.file,
		start: fset.Position(args[idx].Pos()).Offset,
		end:   fset.Position(args[idx+1].Pos()).Offset}
}

// interfaceConflicts lists module interfaces that the method's receiver
// currently satisfies and that declare a method with this name — changing
// the signature would silently break satisfaction at interface call sites,
// which reference resolution cannot see.
func interfaceConflicts(pkgs []*packages.Package, recv *types.Named, methodName string) []string {
	if recv == nil {
		return nil
	}
	ptr := types.NewPointer(recv)
	seen := map[string]bool{}
	var out []string
	for _, p := range pkgs {
		if p.Types == nil || p.ID != p.PkgPath {
			continue // skip test variants: their type universe differs
		}
		scope := p.Types.Scope()
		for _, n := range scope.Names() {
			tn, ok := scope.Lookup(n).(*types.TypeName)
			if !ok {
				continue
			}
			iface, ok := tn.Type().Underlying().(*types.Interface)
			if !ok || iface.NumMethods() == 0 {
				continue
			}
			hasMethod := false
			for i := 0; i < iface.NumMethods(); i++ {
				if iface.Method(i).Name() == methodName {
					hasMethod = true
					break
				}
			}
			if !hasMethod {
				continue
			}
			if types.Implements(ptr, iface) || types.Implements(recv, iface) {
				key := p.Types.Path() + "." + n
				if !seen[key] {
					seen[key] = true
					out = append(out, key)
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

func countSiteFiles(sites []sigCallSite) int {
	files := map[string]bool{}
	for _, s := range sites {
		files[s.file] = true
	}
	return len(files)
}
