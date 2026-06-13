package main

import (
	"go/ast"
	goparser "go/parser"
	"go/token"

	"github.com/chrisophus/gorefactor/orchestrator"
)

// fileDecls parses a Go file and returns the names of its top-level
// declarations: funcs lists functions ("Foo") and methods ("Recv:Method");
// all additionally includes type, var, and const names.
func fileDecls(file string) (funcs, all []string, err error) {
	fset := token.NewFileSet()
	node, perr := goparser.ParseFile(fset, file, nil, goparser.ParseComments)
	if perr != nil {
		return nil, nil, parseErrorf("failed to parse %s: %v", file, perr)
	}
	funcs, all = declNames(node)
	return funcs, all, nil
}

func declNames(node *ast.File) (funcs, all []string) {
	for _, decl := range node.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			name := d.Name.Name
			if d.Recv != nil && len(d.Recv.List) > 0 {
				if recv := receiverTypeName(d.Recv.List[0].Type); recv != "" {
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

// validateFuncTarget checks that the function/method referenced by loc exists
// in file. Returns an exit-3 error when the file does not parse and an
// exit-2 error (with candidates and a did-you-mean hint) when missing.
func validateFuncTarget(file string, loc *orchestrator.InsertionLocation) error {
	if loc == nil || (loc.FunctionName == "" && loc.MethodName == "") {
		return nil
	}
	fset := token.NewFileSet()
	node, err := goparser.ParseFile(fset, file, nil, goparser.ParseComments)
	if err != nil {
		return parseErrorf("failed to parse %s: %v", file, err)
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
	funcs, _ := declNames(node)
	return notFoundError(
		"function "+quoted(name)+" not found in "+file,
		name, funcs)
}

// validateDeclTarget checks that a top-level declaration (function, method,
// type, var, or const) named by spec exists in file.
func validateDeclTarget(file, spec string) error {
	_, all, err := fileDecls(file)
	if err != nil {
		return err
	}
	for _, name := range all {
		if name == spec {
			return nil
		}
	}
	return notFoundError(
		"declaration "+quoted(spec)+" not found in "+file,
		spec, all)
}

func quoted(s string) string { return "\"" + s + "\"" }
