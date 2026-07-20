package main

import (
	goparser "go/parser"
	"go/token"
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
