// Package goload holds the AST / type / package-loading helpers shared by the
// gorefactor CLI and its importable refactoring engines (refactor/extract,
// refactor/changesig). Keeping them here lets the engines load and inspect Go
// source without importing package main.
package goload

import (
	"fmt"
	"go/ast"
	goparser "go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"

	"github.com/chrisophus/gorefactor/internal/cerr"
	"github.com/chrisophus/gorefactor/orchestrator"

	"golang.org/x/tools/go/packages"
)

// LoadTypedPackages loads the module containing file with full syntax and type
// information. includeTests additionally loads _test.go package variants so
// callers can see and rewrite test files.
func LoadTypedPackages(file string, includeTests bool) ([]*packages.Package, string, error) {
	absFile, err := filepath.Abs(file)
	if err != nil {
		return nil, "", fmt.Errorf("abs: %w", err)
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

// PackagesErrors collects load/parse/type errors across packages, capped so
// error messages stay readable.
func PackagesErrors(pkgs []*packages.Package) []string {
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

// FindFileInPackages returns the package and AST for absFile across pkgs.
func FindFileInPackages(pkgs []*packages.Package, absFile string) (*packages.Package, *ast.File) {
	for _, p := range pkgs {
		for i, f := range p.CompiledGoFiles {
			abs, _ := filepath.Abs(f)
			if abs == absFile {
				if i < len(p.Syntax) {
					return p, p.Syntax[i]
				}
			}
		}
		for i, f := range p.GoFiles {
			abs, _ := filepath.Abs(f)
			if abs == absFile {
				if i < len(p.Syntax) {
					return p, p.Syntax[i]
				}
			}
		}
	}
	return nil, nil
}

// LookupNamedType finds a defined (named) type in the package scope. Returns an
// exit-2 error listing the package's type names when missing.
func LookupNamedType(pkg *packages.Package, name string) (*types.Named, error) {
	scope := pkg.Types.Scope()
	obj := scope.Lookup(name)
	if obj == nil {
		var candidates []string
		for _, n := range scope.Names() {
			if _, ok := scope.Lookup(n).(*types.TypeName); ok {
				candidates = append(candidates, n)
			}
		}
		return nil, cerr.NotFound(
			"type "+quoted(name)+" not found in package "+pkg.Types.Name(),
			name, candidates)
	}
	tn, ok := obj.(*types.TypeName)
	if !ok {
		return nil, cerr.NotFoundf("%s is not a type (it is a %T)", name, obj)
	}
	named, ok := tn.Type().(*types.Named)
	if !ok {
		return nil, cerr.NotFoundf("%s is not a defined type", name)
	}
	return named, nil
}

// ZeroValueExpr returns a Go expression producing the zero value of t, rendered
// with typeStr (the type as the user/source spelled it). context.Context gets
// context.TODO(). Returns "" when no safe zero value is known and the caller
// must require an explicit value.
func ZeroValueExpr(t types.Type, typeStr string) string {
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

// SignatureText renders "(a int, b ...string)" and " (int, error)" parts of a
// method signature with named parameters.
func SignatureText(sig *types.Signature, qual types.Qualifier) (params, results string) {
	var ps []string
	for i := 0; i < sig.Params().Len(); i++ {
		p := sig.Params().At(i)
		name := p.Name()
		if name == "" || name == "_" {
			name = fmt.Sprintf("p%d", i)
		}
		ts := types.TypeString(p.Type(), qual)
		if sig.Variadic() && i == sig.Params().Len()-1 {
			ts = "..." + strings.TrimPrefix(ts, "[]")
		}
		ps = append(ps, name+" "+ts)
	}
	params = strings.Join(ps, ", ")
	switch sig.Results().Len() {
	case 0:
	case 1:
		results = " " + types.TypeString(sig.Results().At(0).Type(), qual)
	default:
		var rs []string
		for i := 0; i < sig.Results().Len(); i++ {
			rs = append(rs, types.TypeString(sig.Results().At(i).Type(), qual))
		}
		results = " (" + strings.Join(rs, ", ") + ")"
	}
	return params, results
}

// QualifierFor qualifies types relative to target by package path, which is
// stable across separate type-check universes (e.g. an interface loaded by its
// own packages.Load call).
func QualifierFor(target *types.Package) types.Qualifier {
	return func(other *types.Package) string {
		if other == nil || other.Path() == target.Path() {
			return ""
		}
		return other.Name()
	}
}

// ValidateGoSnippet checks that content parses as a complete Go file, as
// top-level declarations, or as statements. Returns an exit-3 error when none
// of the forms parse.
func ValidateGoSnippet(content string) error {
	fset := token.NewFileSet()
	_, fileErr := goparser.ParseFile(fset, "snippet.go", content, 0)
	if fileErr == nil {
		return nil
	}
	if _, err := goparser.ParseFile(fset, "snippet.go", "package p\n"+content, 0); err == nil {
		return nil
	}
	if _, err := goparser.ParseFile(fset, "snippet.go", "package p\nfunc _() {\n"+content+"\n}", 0); err == nil {
		return nil
	}
	return cerr.Parsef("content does not parse as a Go file, declarations, or statements: %v", fileErr)
}

// ParseFuncLocator splits a "Func" or "Receiver:Method" locator into an
// InsertionLocation.
func ParseFuncLocator(s string) (*orchestrator.InsertionLocation, error) {
	if i := strings.Index(s, ":"); i >= 0 {
		return &orchestrator.InsertionLocation{
			ReceiverType: s[:i],
			MethodName:   s[i+1:],
		}, nil
	}
	return &orchestrator.InsertionLocation{FunctionName: s}, nil
}

// ParseLocatorParts splits a "Func" or "Receiver:Method" locator into its parts.
func ParseLocatorParts(s string) (funcName, methodName, receiverType string) {
	if i := strings.Index(s, ":"); i >= 0 {
		return "", s[i+1:], s[:i]
	}
	return s, "", ""
}

// ReceiverTypeName returns the receiver type name of a method receiver
// expression (dropping any leading pointer).
func ReceiverTypeName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			return id.Name
		}
	case *ast.Ident:
		return t.Name
	}
	return ""
}

// DeclNames returns the names of a file's top-level declarations: funcs lists
// functions ("Foo") and methods ("Recv:Method"); all additionally includes
// type, var, and const names.
func DeclNames(node *ast.File) (funcs, all []string) {
	for _, decl := range node.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			name := d.Name.Name
			if d.Recv != nil && len(d.Recv.List) > 0 {
				if recv := ReceiverTypeName(d.Recv.List[0].Type); recv != "" {
					name = recv + ":" + d.Name.Name
				}
			}
			funcs = append(funcs, name)
			all = append(all, name)
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					all = append(all, s.Name.Name)
				case *ast.ValueSpec:
					for _, n := range s.Names {
						all = append(all, n.Name)
					}
				}
			}
		}
	}
	return funcs, all
}

// ValidateFuncTarget checks that the function/method referenced by loc exists in
// file. Returns an exit-3 error when the file does not parse and an exit-2 error
// (with candidates and a did-you-mean hint) when missing.
func ValidateFuncTarget(file string, loc *orchestrator.InsertionLocation) error {
	if loc == nil || (loc.FunctionName == "" && loc.MethodName == "") {
		return nil
	}
	fset := token.NewFileSet()
	node, err := goparser.ParseFile(fset, file, nil, goparser.ParseComments)
	if err != nil {
		return cerr.Parsef("failed to parse %s: %v", file, err)
	}
	ci := orchestrator.NewCodeInserter()
	if ci.FindFunction(node, loc.FunctionName, loc.MethodName, loc.ReceiverType) != nil {
		return nil
	}
	name := loc.FunctionName
	if name == "" {
		name = loc.MethodName
		if loc.ReceiverType != "" {
			name = loc.ReceiverType + ":" + loc.MethodName
		}
	}
	funcs, _ := DeclNames(node)
	return cerr.NotFound(
		"function "+quoted(name)+" not found in "+file,
		name, funcs)
}

// findModuleRoot walks up from dir looking for go.mod. Falls back to dir itself
// when no module file is found (GOPATH-less ad-hoc package).
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

func quoted(s string) string { return "\"" + s + "\"" }
