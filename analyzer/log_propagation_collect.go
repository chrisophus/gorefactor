package analyzer

import (
	"go/ast"
	"go/token"
	"strings"
)

func collectErrorsNewSentinels(files []*ast.File, paths []string) map[string]bool {
	out := make(map[string]bool)
	for i, f := range files {
		if strings.HasSuffix(paths[i], "_test.go") {
			continue
		}
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok || vs.Names == nil || len(vs.Values) == 0 {
					continue
				}
				for j := range vs.Names {
					if vs.Names[j].Name == "_" || j >= len(vs.Values) {
						continue
					}
					if isErrorsNewCall(vs.Values[j]) {
						out[vs.Names[j].Name] = true
					}
				}
			}
		}
	}
	return out
}
