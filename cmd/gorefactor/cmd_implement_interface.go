package main

import (
	"fmt"
	goparser "go/parser"
	"go/token"
	"go/types"
	"os"
	"strings"

	"github.com/chrisophus/gorefactor/orchestrator"
	"golang.org/x/tools/go/packages"
)

var implementInterfaceFlags = mutFlagSpec(map[string]bool{"--iface-in": true})

func init() {
	registerCommand(Command{
		Name:        "implement-interface",
		Description: "Generate compiling method stubs on a type for every interface method it doesn't implement yet",
		Usage:       "implement-interface <file> <Type> <Interface> [--iface-in path] [--json] [--dry-run] [--gate]",
		MinArgs:     3,
		MaxArgs:     3,
		Flags:       implementInterfaceFlags,
		Run:         implementInterfaceCommand,
	})
}

func implementInterfaceCommand(args []string) error {
	pos, flags := parseFlags(args, implementInterfaceFlags)
	if len(pos) < 3 {
		return usageErrorf("usage: implement-interface <file> <Type> <Interface> [--iface-in path]")
	}
	file, typeName, ifaceArg := pos[0], pos[1], pos[2]
	m := &mutation{op: "implement-interface", file: file}
	m.setCommonFlags(flags)

	pkgs, absFile, err := loadTypedPackages(file, false)
	if err != nil {
		return m.fail(err)
	}
	pkg, _ := findFileInPackages(pkgs, absFile)
	if pkg == nil {
		return m.fail(notFoundErrorf("file %s not in any loaded package", file))
	}
	named, err := lookupNamedType(pkg, typeName)
	if err != nil {
		return m.fail(err)
	}
	if named.TypeParams() != nil && named.TypeParams().Len() > 0 {
		return m.fail(usageErrorf("implement-interface does not support generic types"))
	}
	iface, ifaceName, err := resolveInterface(pkg, ifaceArg, flags["--iface-in"])
	if err != nil {
		return m.fail(err)
	}

	stubs, warnings, err := missingMethodStubs(pkg, named, typeName, iface, ifaceName)
	if err != nil {
		return m.fail(err)
	}
	if len(stubs) == 0 {
		detail := fmt.Sprintf("%s already implements %s; nothing to do", typeName, ifaceName)
		if len(warnings) > 0 {
			detail += "\nwarning:\n  " + strings.Join(warnings, "\n  ")
		}
		return m.run(func() (string, error) { return detail, nil })
	}
	code := "\n" + strings.Join(stubs, "\n")
	if err := validateGoSnippet(code); err != nil {
		return m.fail(err)
	}
	return m.run(func() (string, error) {
		src, err := os.ReadFile(file)
		if err != nil {
			return "", err
		}
		out := append(src, []byte(code)...)
		fset := token.NewFileSet()
		if _, perr := goparser.ParseFile(fset, file, out, 0); perr != nil {
			return "", parseErrorf("internal: stubbed file does not parse, refusing to write: %v", perr)
		}
		if err := os.WriteFile(file, out, 0644); err != nil {
			return "", err
		}
		if err := orchestrator.FormatImports(file); err != nil {
			fmt.Fprintf(os.Stderr, "warning: format imports on %s: %v\n", file, err)
		}
		detail := fmt.Sprintf("Added %d method stub(s) to %s for %s", len(stubs), typeName, ifaceName)
		if len(warnings) > 0 {
			detail += "\nwarning:\n  " + strings.Join(warnings, "\n  ")
		}
		return detail, nil
	})
}

// resolveInterface resolves an interface by bare name (same package),
// qualified import path ("io.Reader", "net/http.Handler"), or a directory
// passed via --iface-in.
func resolveInterface(pkg *packages.Package, arg, ifaceIn string) (*types.Interface, string, error) {
	var scope *types.Scope
	name := arg
	display := arg
	switch {
	case ifaceIn != "":
		cfg := &packages.Config{Mode: packages.NeedName | packages.NeedTypes, Dir: ifaceIn}
		loaded, err := packages.Load(cfg, ".")
		if err != nil || len(loaded) == 0 || loaded[0].Types == nil {
			return nil, "", notFoundErrorf("cannot load package in --iface-in %s: %v", ifaceIn, err)
		}
		scope = loaded[0].Types.Scope()
		display = loaded[0].Types.Name() + "." + name
	case strings.Contains(arg, "."):
		dot := strings.LastIndex(arg, ".")
		path, n := arg[:dot], arg[dot+1:]
		cfg := &packages.Config{Mode: packages.NeedName | packages.NeedTypes}
		loaded, err := packages.Load(cfg, path)
		if err != nil || len(loaded) == 0 || loaded[0].Types == nil || len(loaded[0].Errors) > 0 {
			return nil, "", notFoundErrorf("cannot load package %q for interface %s (try --iface-in <dir> for module-local packages)", path, arg)
		}
		scope = loaded[0].Types.Scope()
		name = n
	default:
		scope = pkg.Types.Scope()
	}
	obj := scope.Lookup(name)
	if obj == nil {
		var candidates []string
		for _, n := range scope.Names() {
			if tn, ok := scope.Lookup(n).(*types.TypeName); ok {
				if _, isIface := tn.Type().Underlying().(*types.Interface); isIface {
					candidates = append(candidates, n)
				}
			}
		}
		return nil, "", notFoundError("interface "+quoted(name)+" not found", name, candidates)
	}
	tn, ok := obj.(*types.TypeName)
	if !ok {
		return nil, "", notFoundErrorf("%s is not a type", display)
	}
	iface, ok := tn.Type().Underlying().(*types.Interface)
	if !ok {
		return nil, "", notFoundErrorf("%s is not an interface (underlying: %s)", display, tn.Type().Underlying())
	}
	return iface, display, nil
}

// missingMethodStubs renders a stub for each interface method the type's
// (pointer) method set lacks. Methods present with a different signature are
// reported as warnings rather than stubbed (a duplicate name would not
// compile).
func missingMethodStubs(pkg *packages.Package, named *types.Named, typeName string, iface *types.Interface, ifaceName string) (stubs, warnings []string, err error) {
	qual := qualifierFor(pkg.Types)
	existing := map[string]string{}
	mset := types.NewMethodSet(types.NewPointer(named))
	for i := 0; i < mset.Len(); i++ {
		if f, ok := mset.At(i).Obj().(*types.Func); ok {
			existing[f.Name()] = types.TypeString(f.Type(), qual)
		}
	}
	recvName, ptrRecv := receiverStyle(named, typeName)
	recvType := typeName
	if ptrRecv {
		recvType = "*" + typeName
	}
	for i := 0; i < iface.NumMethods(); i++ {
		im := iface.Method(i)
		if !im.Exported() && im.Pkg() != nil && im.Pkg().Path() != pkg.Types.Path() {
			return nil, nil, notFoundErrorf(
				"interface %s has unexported method %s from package %s; it cannot be implemented outside that package",
				ifaceName, im.Name(), im.Pkg().Path())
		}
		if have, ok := existing[im.Name()]; ok {
			want := types.TypeString(im.Type(), qual)
			if have != want {
				warnings = append(warnings,
					fmt.Sprintf("method %s exists with signature %s but %s wants %s — not stubbed", im.Name(), have, ifaceName, want))
			}
			continue
		}
		params, results := signatureText(im.Type().(*types.Signature), qual)
		stubs = append(stubs, fmt.Sprintf(
			"// %s implements %s.\nfunc (%s %s) %s(%s)%s {\n\tpanic(\"unimplemented\")\n}\n",
			im.Name(), ifaceName, recvName, recvType, im.Name(), params, results))
	}
	return stubs, warnings, nil
}

// receiverStyle picks the receiver name and pointer-ness matching the
// type's existing methods (pointer receiver by default).
func receiverStyle(named *types.Named, typeName string) (name string, pointer bool) {
	name = strings.ToLower(typeName[:1])
	if named.NumMethods() == 0 {
		return name, true
	}
	pointer = false
	for i := 0; i < named.NumMethods(); i++ {
		sig := named.Method(i).Type().(*types.Signature)
		if recv := sig.Recv(); recv != nil {
			if _, isPtr := recv.Type().(*types.Pointer); isPtr {
				pointer = true
			}
			if n := recv.Name(); n != "" && n != "_" && name == strings.ToLower(typeName[:1]) {
				name = n
			}
		}
	}
	return name, pointer
}

// signatureText renders "(a int, b ...string)" and " (int, error)" parts of
// a method signature with named parameters.
func signatureText(sig *types.Signature, qual types.Qualifier) (params, results string) {
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

// qualifierFor qualifies types relative to target by package path, which is
// stable across separate type-check universes (e.g. an interface loaded by
// its own packages.Load call).
func qualifierFor(target *types.Package) types.Qualifier {
	return func(other *types.Package) string {
		if other == nil || other.Path() == target.Path() {
			return ""
		}
		return other.Name()
	}
}
