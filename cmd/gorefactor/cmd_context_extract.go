package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
)

// extractDeclContext returns the full source (incl. doc comment) of the named
// declaration in file, its doc text, and the FuncType when it is a function.
func extractDeclContext(file, name, recv string) (source, doc string, fnType *ast.FuncType) {
	src, err := os.ReadFile(file)
	if err != nil {
		return "", "", nil
	}
	fset := token.NewFileSet()
	astFile, err := parser.ParseFile(fset, file, src, parser.ParseComments)
	if err != nil {
		return "", "", nil
	}
	slice := func(start, end token.Pos) string {
		return strings.TrimSpace(string(src[fset.Position(start).Offset:fset.Position(end).Offset]))
	}
	for _, decl := range astFile.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Name.Name != name || cgReceiver(d) != strings.TrimPrefix(recv, "*") {
				continue
			}
			start := d.Pos()
			if d.Doc != nil {
				start = d.Doc.Pos()
			}
			return slice(start, d.End()), docText(d.Doc), d.Type
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				if !specHasName(spec, name) {
					continue
				}
				start := d.Pos()
				if d.Doc != nil {
					start = d.Doc.Pos()
				}
				return slice(start, d.End()), docText(d.Doc), nil
			}
		}
	}
	return "", "", nil
}
