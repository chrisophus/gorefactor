package main

import (
	"fmt"
	"go/ast"
	goparser "go/parser"
	"go/token"
	"os"
	"strings"

	"github.com/chrisophus/gorefactor/orchestrator"
)

var changeReceiverFlags = mutFlagSpec(map[string]bool{
	"--pointer": false,
	"--value":   false,
})

func init() {
	registerCommand(Command{
		Name:        "change-receiver",
		Mutates:     true,
		MCPTool:     true,
		TxnSafe:     true,
		Description: "Switch a method's receiver between value and pointer form",
		Usage:       "change-receiver <file> <Type:Method> --pointer|--value [--json] [--dry-run] [--gate]",
		MinArgs:     2,
		MaxArgs:     2,
		Flags:       changeReceiverFlags,
		Run:         changeReceiverCommand,
	})
}

// changeReceiverCommand rewrites a method's receiver declaration between
// `(r T)` and `(r *T)`. It warns (without failing) about copy-semantics
// implications and about same-package interfaces that declare a method of
// the same name, since the method set of T changes. Cross-package interface
// satisfaction is not checked — run with --gate (or `gorefactor doctor`) to
// type-check the build.
func changeReceiverCommand(args []string) error {
	pos, flags := parseFlags(args, changeReceiverFlags)
	if len(pos) < 2 {
		return usageErrorf("usage: change-receiver <file> <Type:Method> --pointer|--value")
	}
	file := pos[0]
	locator := pos[1]
	toPointer := flags["--pointer"] != ""
	toValue := flags["--value"] != ""
	if toPointer == toValue {
		return usageErrorf("change-receiver requires exactly one of --pointer or --value")
	}
	i := strings.Index(locator, ":")
	if i <= 0 || i == len(locator)-1 {
		return usageErrorf("change-receiver target must be Type:Method (got %q)", locator)
	}
	typeName, methodName := locator[:i], locator[i+1:]

	m := &mutation{op: "change-receiver", file: file}
	m.setCommonFlags(flags)

	src, err := os.ReadFile(file)
	if err != nil {
		return m.fail(err)
	}
	fset := token.NewFileSet()
	node, err := goparser.ParseFile(fset, file, src, goparser.ParseComments)
	if err != nil {
		return m.fail(parseErrorf("failed to parse %s: %v", file, err))
	}

	var method *ast.FuncDecl
	method = findReceiverMethod(node, methodName, typeName, method)
	if method == nil {
		funcs, _ := declNames(node)
		return m.fail(notFoundError(
			fmt.Sprintf("method %q not found in %s", locator, file),
			locator, funcs))
	}

	recvField := method.Recv.List[0]
	_, isPointer := recvField.Type.(*ast.StarExpr)
	if toPointer && isPointer {
		return m.run(func() (string, error) {
			return fmt.Sprintf("%s already has a pointer receiver — no change", locator), nil
		})
	}
	if toValue && !isPointer {
		return m.run(func() (string, error) {
			return fmt.Sprintf("%s already has a value receiver — no change", locator), nil
		})
	}

	typeStart, typeEnd, newType := buildReceiverType(fset, recvField, toPointer, src)

	var out []byte
	out = append(out, src[:typeStart]...)
	out = append(out, []byte(newType)...)
	out = append(out, src[typeEnd:]...)
	if _, perr := goparser.ParseFile(token.NewFileSet(), file, out, 0); perr != nil {
		return m.fail(parseErrorf("receiver change would produce a malformed file: %v", perr))
	}

	// Semantics warnings (best-effort, never fatal).
	emitReceiverWarnings(method, typeName, methodName, toPointer)
	warnSamePackageInterfaces(file, typeName, methodName)

	return m.run(func() (string, error) {
		return writeChangedReceiver(file, locator, out, toPointer)
	})
}
func writeChangedReceiver(file, locator string, out []byte, toPointer bool) (string, error) {
	if err := os.WriteFile(file, out, 0644); err != nil {
		return "", err
	}
	if err := orchestrator.FormatImports(file); err != nil {
		fmt.Fprintf(os.Stderr, "warning: format imports on %s: %v\n", file, err)
	}
	form := "value"
	if toPointer {
		form = "pointer"
	}
	return fmt.Sprintf("Changed %s to a %s receiver in %s", locator, form, file), nil
}

func warnSamePackageInterfaces(file, typeName, methodName string) {
	if ifaces := samePackageInterfacesDeclaring(file, methodName); len(ifaces) > 0 {
		fmt.Fprintf(os.Stderr,
			"warning: interface(s) in this package declare a method %q: %s — the method set of %s changes; verify assignments still compile (cross-package interfaces are not checked; use --gate or `gorefactor doctor`)\n",
			methodName, strings.Join(ifaces, ", "), typeName)
	}
}

func buildReceiverType(fset *token.FileSet, recvField *ast.Field, toPointer bool, src []byte) (int, int, string) {
	typeStart := fset.Position(recvField.Type.Pos()).Offset
	typeEnd := fset.Position(recvField.Type.End()).Offset
	var newType string
	if toPointer {
		newType = "*" + string(src[typeStart:typeEnd])
	} else {
		star := recvField.Type.(*ast.StarExpr)
		xs := fset.Position(star.X.Pos()).Offset
		xe := fset.Position(star.X.End()).Offset
		newType = string(src[xs:xe])
	}
	return typeStart, typeEnd, newType
}

func findReceiverMethod(node *ast.File, methodName string, typeName string, method *ast.FuncDecl) *ast.FuncDecl {
	for _, d := range node.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Recv == nil || len(fd.Recv.List) == 0 || fd.Name.Name != methodName {
			continue
		}
		if receiverTypeName(fd.Recv.List[0].Type) == typeName {
			method = fd
			break
		}
	}
	return method
}

// emitReceiverWarnings prints copy-semantics warnings for the direction of
// the change.
func emitReceiverWarnings(method *ast.FuncDecl, typeName, methodName string, toPointer bool) {
	recvName := ""
	if len(method.Recv.List[0].Names) > 0 {
		recvName = method.Recv.List[0].Names[0].Name
	}
	if toPointer {
		fmt.Fprintf(os.Stderr,
			"warning: with a pointer receiver, %s is no longer in the method set of %s values; interface assignments of non-pointer %s values and calls on non-addressable values (map elements, rvalues) will stop compiling\n",
			methodName, typeName, typeName)
		return
	}
	// pointer -> value: mutations now act on a copy.
	if recvName != "" && receiverIsMutated(method, recvName) {
		fmt.Fprintf(os.Stderr,
			"warning: %s mutates its receiver; with a value receiver those mutations apply to a copy and are lost at return\n",
			methodName)
	} else {
		fmt.Fprintf(os.Stderr,
			"warning: with a value receiver, %s receives a copy of %s; any aliasing or mutation semantics change\n",
			methodName, typeName)
	}
}

// receiverIsMutated reports whether the method body assigns to the receiver
// or any field/element reachable through it, or takes its address.
func receiverIsMutated(method *ast.FuncDecl, recvName string) bool {
	if method.Body == nil {
		return false
	}
	rootedInRecv := func(e ast.Expr) bool {
		for {
			switch v := e.(type) {
			case *ast.Ident:
				return v.Name == recvName
			case *ast.SelectorExpr:
				e = v.X
			case *ast.IndexExpr:
				e = v.X
			case *ast.StarExpr:
				e = v.X
			case *ast.ParenExpr:
				e = v.X
			default:
				return false
			}
		}
	}
	mutated := false
	ast.Inspect(method.Body, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.AssignStmt:
			for _, lhs := range v.Lhs {
				if rootedInRecv(lhs) {
					mutated = true
				}
			}
		case *ast.IncDecStmt:
			if rootedInRecv(v.X) {
				mutated = true
			}
		case *ast.UnaryExpr:
			if v.Op == token.AND && rootedInRecv(v.X) {
				mutated = true
			}
		}
		return !mutated
	})
	return mutated
}

// samePackageInterfacesDeclaring lists interfaces declared in the package of
// file that include a method named methodName.
func samePackageInterfacesDeclaring(file, methodName string) []string {
	var ifaces []string
	for _, f := range packageGoFiles(file) {
		fset := token.NewFileSet()
		node, err := goparser.ParseFile(fset, f, nil, 0)
		if err != nil {
			continue
		}
		for _, d := range node.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, s := range gd.Specs {
				ts, ok := s.(*ast.TypeSpec)
				if !ok {
					continue
				}
				it, ok := ts.Type.(*ast.InterfaceType)
				if !ok || it.Methods == nil {
					continue
				}
				for _, mf := range it.Methods.List {
					for _, n := range mf.Names {
						if n.Name == methodName {
							ifaces = append(ifaces, ts.Name.Name)
						}
					}
				}
			}
		}
	}
	return ifaces
}
