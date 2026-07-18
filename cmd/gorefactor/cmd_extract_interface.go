package main

import (
	"fmt"
	"go/types"
	"os"
	"sort"
	"strings"

	"github.com/chrisophus/gorefactor/orchestrator"
)

var extractInterfaceFlags = mutFlagSpec(map[string]bool{"--methods": true})

func init() {
	registerCommand(Command{
		Name:        "extract-interface",
		Description: "Generate an interface declaration from a concrete type's exported method set",
		Usage:       "extract-interface <file> <Type> <NewInterfaceName> [--methods m1,m2,...] [--json] [--dry-run] [--gate]",
		MinArgs:     3,
		MaxArgs:     3,
		Flags:       extractInterfaceFlags,
		Run:         extractInterfaceCommand,
	})
}

func extractInterfaceCommand(args []string) error {
	pos, flags := parseFlags(args, extractInterfaceFlags)
	if len(pos) < 3 {
		return usageErrorf("usage: extract-interface <file> <Type> <NewInterfaceName> [--methods m1,m2,...]")
	}
	file, typeName, ifaceName := pos[0], pos[1], pos[2]
	m := &mutation{op: "extract-interface", file: file}
	m.setCommonFlags(flags)

	methodFilter := extractIfaceParseMethodFilter(flags["--methods"])

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

	// Check the interface name doesn't already exist.
	if obj := pkg.Types.Scope().Lookup(ifaceName); obj != nil {
		return m.fail(notFoundErrorf("name %q already declared in this package", ifaceName))
	}

	qual := qualifierFor(pkg.Types)
	// Collect exported methods from the pointer method set.
	mset := types.NewMethodSet(types.NewPointer(named))
	methods := extractIfaceCollectMethods(mset, methodFilter, qual)
	// Report unknown --methods entries.
	if ferr := extractIfaceUnknownFilterError(mset, methodFilter, methods, typeName); ferr != nil {
		return m.fail(ferr)
	}

	if len(methods) == 0 {
		return m.fail(notFoundErrorf("%s has no exported methods%s to extract",
			typeName, func() string {
				if methodFilter != nil {
					return " matching --methods filter"
				}
				return ""
			}()))
	}

	ifaceCode := extractIfaceRender(ifaceName, typeName, methods)

	if err := validateGoSnippet(ifaceCode); err != nil {
		return m.fail(err)
	}

	return m.run(func() (string, error) {
		src, err := os.ReadFile(file)
		if err != nil {
			return "", err
		}
		out := append(src, []byte(ifaceCode)...)
		if err := os.WriteFile(file, out, 0644); err != nil {
			return "", err
		}
		if err := orchestrator.FormatImports(file); err != nil {
			fmt.Fprintf(os.Stderr, "warning: format imports on %s: %v\n", file, err)
		}
		hint := fmt.Sprintf("Added interface %s (%d method(s)) to %s\nhint: run 'gorefactor find-implementations %s' to see all types that satisfy this interface",
			ifaceName, len(methods), file, ifaceName)
		return hint, nil
	})

}

type ifaceMethodEntry struct {
	name string
	text string
}

func extractIfaceParseMethodFilter(mlist string) map[string]bool {
	if mlist == "" {
		return nil
	}
	methodFilter := make(map[string]bool)
	for _, name := range strings.Split(mlist, ",") {
		name = strings.TrimSpace(name)
		if name != "" {
			methodFilter[name] = true
		}
	}
	return methodFilter
}

func extractIfaceCollectMethods(mset *types.MethodSet, methodFilter map[string]bool, qual types.Qualifier) []ifaceMethodEntry {
	var methods []ifaceMethodEntry
	for i := 0; i < mset.Len(); i++ {
		sel := mset.At(i)
		fn, ok := sel.Obj().(*types.Func)
		if !ok || !fn.Exported() {
			continue
		}
		if methodFilter != nil && !methodFilter[fn.Name()] {
			continue
		}
		sig := fn.Type().(*types.Signature)
		params, results := signatureText(sig, qual)
		var sb strings.Builder
		sb.WriteString("\t")
		sb.WriteString(fn.Name())
		sb.WriteString("(")
		sb.WriteString(params)
		sb.WriteString(")")
		if results != "" {
			sb.WriteString(results)
		}
		methods = append(methods, ifaceMethodEntry{name: fn.Name(), text: sb.String()})
	}
	return methods
}

func extractIfaceUnknownFilterError(mset *types.MethodSet, methodFilter map[string]bool, methods []ifaceMethodEntry, typeName string) error {
	if methodFilter == nil {
		return nil
	}
	known := make(map[string]bool, len(methods))
	for _, me := range methods {
		known[me.name] = true
	}
	var unknown []string
	for name := range methodFilter {
		if !known[name] {
			unknown = append(unknown, name)
		}
	}
	sort.Strings(unknown)
	if len(unknown) == 0 {
		return nil
	}

	var available []string
	for i := 0; i < mset.Len(); i++ {
		if fn, ok := mset.At(i).Obj().(*types.Func); ok && fn.Exported() {
			available = append(available, fn.Name())
		}
	}
	sort.Strings(available)
	return notFoundError(
		fmt.Sprintf("method(s) %s not found on %s", strings.Join(unknown, ", "), typeName),
		unknown[0], available)
}

func extractIfaceRender(ifaceName, typeName string, methods []ifaceMethodEntry) string {
	sort.Slice(methods, func(i, j int) bool { return methods[i].name < methods[j].name })
	var sb strings.Builder
	fmt.Fprintf(&sb, "\n// %s is implemented by %s.\ntype %s interface {\n", ifaceName, typeName, ifaceName)
	for _, me := range methods {
		sb.WriteString(me.text)
		sb.WriteString("\n")
	}
	sb.WriteString("}\n")
	return sb.String()
}
