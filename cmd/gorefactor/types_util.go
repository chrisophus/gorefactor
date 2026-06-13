package main

import (
	"fmt"
	"go/types"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/packages"
)

// findModuleRoot walks up from dir looking for go.mod. Falls back to dir
// itself when no module file is found (GOPATH-less ad-hoc package).
func findModuleRoot(dir string) string {
	d := dir
	for {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d
		}
		parent := filepath.Dir(d)
		if parent == d {
			return dir
		}
		d = parent
	}
}

// loadTypedPackages loads the module containing file with full syntax and
// type information. includeTests additionally loads _test.go package
// variants so callers can see and rewrite test files.
func loadTypedPackages(file string, includeTests bool) ([]*packages.Package, string, error) {
	absFile, err := filepath.Abs(file)
	if err != nil {
		return nil, "", err
	}
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
			packages.NeedTypes | packages.NeedTypesInfo | packages.NeedCompiledGoFiles |
			packages.NeedDeps | packages.NeedImports,
		Dir:   findModuleRoot(filepath.Dir(absFile)),
		Tests: includeTests,
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, "", fmt.Errorf("load packages: %w", err)
	}
	return pkgs, absFile, nil
}

// packagesErrors collects load/parse/type errors across packages, capped so
// error messages stay readable.
func packagesErrors(pkgs []*packages.Package) []string {
	var msgs []string
	for _, p := range pkgs {
		for _, e := range p.Errors {
			msgs = append(msgs, e.Error())
			if len(msgs) >= 5 {
				return msgs
			}
		}
	}
	return msgs
}

// lookupNamedType finds a defined (named) type in the package scope.
// Returns an exit-2 error listing the package's type names when missing.
func lookupNamedType(pkg *packages.Package, name string) (*types.Named, error) {
	scope := pkg.Types.Scope()
	obj := scope.Lookup(name)
	if obj == nil {
		var candidates []string
		for _, n := range scope.Names() {
			if _, ok := scope.Lookup(n).(*types.TypeName); ok {
				candidates = append(candidates, n)
			}
		}
		return nil, notFoundError(
			"type "+quoted(name)+" not found in package "+pkg.Types.Name(),
			name, candidates)
	}
	tn, ok := obj.(*types.TypeName)
	if !ok {
		return nil, notFoundErrorf("%s is not a type (it is a %T)", name, obj)
	}
	named, ok := tn.Type().(*types.Named)
	if !ok {
		return nil, notFoundErrorf("%s is not a defined type", name)
	}
	return named, nil
}

// zeroValueExpr returns a Go expression producing the zero value of t,
// rendered with typeStr (the type as the user/source spelled it).
// context.Context gets context.TODO(). Returns "" when no safe zero value
// is known and the caller must require an explicit value.
func zeroValueExpr(t types.Type, typeStr string) string {
	if t != nil && types.TypeString(t, nil) == "context.Context" {
		return "context.TODO()"
	}
	if strings.TrimSpace(typeStr) == "context.Context" {
		return "context.TODO()"
	}
	if t == nil {
		return zeroValueFromSyntax(typeStr)
	}
	switch u := t.Underlying().(type) {
	case *types.Basic:
		info := u.Info()
		switch {
		case info&types.IsNumeric != 0:
			return "0"
		case info&types.IsString != 0:
			return `""`
		case info&types.IsBoolean != 0:
			return "false"
		case u.Kind() == types.UnsafePointer:
			return "nil"
		}
		return ""
	case *types.Pointer, *types.Slice, *types.Map, *types.Chan, *types.Signature, *types.Interface:
		return "nil"
	case *types.Struct, *types.Array:
		return strings.TrimSpace(typeStr) + "{}"
	}
	return ""
}

// zeroValueFromSyntax guesses a zero value from the spelling of a type when
// type checking could not resolve it.
func zeroValueFromSyntax(typeStr string) string {
	s := strings.TrimSpace(typeStr)
	switch {
	case strings.HasPrefix(s, "*"), strings.HasPrefix(s, "[]"),
		strings.HasPrefix(s, "map["), strings.HasPrefix(s, "chan "),
		strings.HasPrefix(s, "chan"), strings.HasPrefix(s, "func"),
		s == "any", s == "interface{}", s == "error":
		return "nil"
	case s == "string":
		return `""`
	case s == "bool":
		return "false"
	case s == "int", s == "int8", s == "int16", s == "int32", s == "int64",
		s == "uint", s == "uint8", s == "uint16", s == "uint32", s == "uint64",
		s == "uintptr", s == "byte", s == "rune", s == "float32", s == "float64",
		s == "complex64", s == "complex128":
		return "0"
	}
	return ""
}
