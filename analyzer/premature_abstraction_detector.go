package analyzer

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
)

// PrematureAbstractionIssue flags a function that returns an interface
// declared in the same package — the "return structs" half of the Go
// idiom. When the abstraction is declared and consumed within one
// package, returning the concrete struct is usually preferable; the
// interface adds friction without yielding substitutability.
type PrematureAbstractionIssue struct {
	Function  string
	Interface string
	File      string
	Line      int
	Message   string
}

// FindPrematureAbstractionsInDir scans dir as a single package, finds
// every interface declared locally, then flags functions returning one
// whose interface has exactly one implementation in the same package.
// Implementation count uses a method-name overlap heuristic — fast
// enough to run per-file and accurate enough to suppress noise from
// interfaces with multiple real impls (e.g. Provider with mock/openai/
// anthropic backends).
func FindPrematureAbstractionsInDir(dir string) ([]PrematureAbstractionIssue, error) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, nil, 0)
	if err != nil {
		return nil, err
	}

	var out []PrematureAbstractionIssue
	for _, pkg := range pkgs {
		localIfaces := localInterfaceMethods(pkg)
		if len(localIfaces) == 0 {
			continue
		}
		methodsByReceiver := methodsByReceiver(pkg)
		implCount := countImpls(localIfaces, methodsByReceiver)

		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Type.Results == nil {
					continue
				}
				ifaceName := returnedLocalInterface(fn, localIfaces)
				if ifaceName == "" {
					continue
				}
				if implCount[ifaceName] != 1 {
					continue
				}
				pos := fset.Position(fn.Pos())
				out = append(out, PrematureAbstractionIssue{
					Function:  fn.Name.Name,
					Interface: ifaceName,
					File:      pos.Filename,
					Line:      pos.Line,
					Message: fmt.Sprintf(
						"%s returns local interface %s with a single implementation — return the concrete struct unless polymorphism is imminent",
						fn.Name.Name, ifaceName,
					),
				})
			}
		}
	}
	return out, nil
}

// methodsByReceiver maps each receiver type name → set of its method
// names declared in the package.
func methodsByReceiver(pkg *ast.Package) map[string]map[string]bool {
	out := make(map[string]map[string]bool)
	for _, file := range pkg.Files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || len(fn.Recv.List) == 0 {
				continue
			}
			recv := getReceiverTypeName(fn.Recv.List[0])
			if recv == "" {
				continue
			}
			if out[recv] == nil {
				out[recv] = make(map[string]bool)
			}
			out[recv][fn.Name.Name] = true
		}
	}
	return out
}

// countImpls returns interface name → number of receiver types in the
// package whose method-name set is a superset of the interface's. This
// is a name-only check (no signature match) — fast and good enough.
func countImpls(ifaceMethods map[string]map[string]bool, methodsByRecv map[string]map[string]bool) map[string]int {
	out := make(map[string]int, len(ifaceMethods))
	for iface, methods := range ifaceMethods {
		if len(methods) == 0 {
			continue
		}
		for _, recvMethods := range methodsByRecv {
			ok := true
			for m := range methods {
				if !recvMethods[m] {
					ok = false
					break
				}
			}
			if ok {
				out[iface]++
			}
		}
	}
	return out
}

// localInterfaceMethods maps each locally-declared interface name → set
// of its method names. An interface with zero methods (e.g. any) is
// recorded as an empty set; countImpls skips those.
func localInterfaceMethods(pkg *ast.Package) map[string]map[string]bool {
	out := make(map[string]map[string]bool)
	for _, file := range pkg.Files {
		for _, decl := range file.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				it, ok := ts.Type.(*ast.InterfaceType)
				if !ok {
					continue
				}
				methods := make(map[string]bool)
				if it.Methods != nil {
					for _, m := range it.Methods.List {
						for _, n := range m.Names {
							methods[n.Name] = true
						}
					}
				}
				out[ts.Name.Name] = methods
			}
		}
	}
	return out
}

func returnedLocalInterface(fn *ast.FuncDecl, localIfaces map[string]map[string]bool) string {
	for _, ret := range fn.Type.Results.List {
		switch t := ret.Type.(type) {
		case *ast.Ident:
			if _, ok := localIfaces[t.Name]; ok {
				return t.Name
			}
		case *ast.StarExpr:
			if ident, ok := t.X.(*ast.Ident); ok {
				if _, ok := localIfaces[ident.Name]; ok {
					return ident.Name
				}
			}
		}
	}
	return ""
}
