package analyzer

import (
	"fmt"
	"go/ast"
	"go/token"
	"path/filepath"

	"golang.org/x/tools/go/packages"
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
	cfg := &packages.Config{
		Mode: packages.NeedSyntax | packages.NeedFiles,
		Dir:  dir,
	}
	pkgs, err := packages.Load(cfg, ".")
	if err != nil {
		return nil, fmt.Errorf("load: %w", err)
	}
	return findPrematureAbstractions(pkgs), nil
}

// FindPrematureAbstractionsInDirs scans every directory in one packages.Load
// call instead of one toolchain invocation per directory. Passing the explicit
// directory list as patterns (rather than "./...") keeps the scanned set
// identical to the caller's walk-filtered file set.
func FindPrematureAbstractionsInDirs(dirs []string) ([]PrematureAbstractionIssue, error) {
	if len(dirs) == 0 {
		return nil, nil
	}
	patterns := make([]string, 0, len(dirs))
	for _, d := range dirs {
		switch {
		case d == "" || d == ".":
			patterns = append(patterns, ".")
		case filepath.IsAbs(d):
			patterns = append(patterns, d)
		default:
			patterns = append(patterns, "./"+filepath.ToSlash(d))
		}
	}
	cfg := &packages.Config{Mode: packages.NeedSyntax | packages.NeedFiles}
	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		return nil, fmt.Errorf("load: %w", err)
	}
	return findPrematureAbstractions(pkgs), nil
}

// findPrematureAbstractions runs the detection heuristic over already-loaded
// packages.
func findPrematureAbstractions(pkgs []*packages.Package) []PrematureAbstractionIssue {
	var out []PrematureAbstractionIssue
	for _, pkg := range pkgs {
		if len(pkg.Errors) > 0 {
			continue
		}

		localIfaces := localInterfaceMethodsFromPackage(pkg)
		if len(localIfaces) == 0 {
			continue
		}
		methodsByRecv := methodsByReceiverFromPackage(pkg)
		implCount := countImpls(localIfaces, methodsByRecv)

		for _, syntax := range pkg.Syntax {
			for _, decl := range syntax.Decls {
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
				pos := pkg.Fset.Position(fn.Pos())
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
	return out
}

// methodsByReceiverFromPackage maps each receiver type name → set of its method
// names declared in the package.
func methodsByReceiverFromPackage(pkg *packages.Package) map[string]map[string]bool {
	out := make(map[string]map[string]bool)
	for _, syntax := range pkg.Syntax {
		for _, decl := range syntax.Decls {
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

// localInterfaceMethodsFromPackage maps each locally-declared interface name → set
// of its method names. An interface with zero methods (e.g. any) is
// recorded as an empty set; countImpls skips those.
func localInterfaceMethodsFromPackage(pkg *packages.Package) map[string]map[string]bool {
	out := make(map[string]map[string]bool)
	for _, syntax := range pkg.Syntax {
		for _, decl := range syntax.Decls {
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
