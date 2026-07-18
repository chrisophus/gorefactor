package orchestrator

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// packageGoFiles lists the .go files directly inside a directory.
func packageGoFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	return files, nil
}

// packageLevelSymbols collects the names declared at package level in a
// directory's package: functions (not methods), types, consts and vars.
// Values are true when the symbol is exported.
func packageLevelSymbols(fset *token.FileSet, dir, pkgName string) (map[string]bool, error) {
	files, err := packageGoFiles(dir)
	if err != nil {
		return nil, err
	}
	symbols := map[string]bool{}
	for _, path := range files {
		node, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil || node.Name.Name != pkgName {
			continue
		}
		collectPackageLevelSymbols(node, symbols)
	}
	return symbols, nil

}

func collectPackageLevelSymbols(node *ast.File, symbols map[string]bool) {
	for _, decl := range node.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil {
				symbols[d.Name.Name] = ast.IsExported(d.Name.Name)
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					symbols[s.Name.Name] = ast.IsExported(s.Name.Name)
				case *ast.ValueSpec:
					for _, nm := range s.Names {
						symbols[nm.Name] = ast.IsExported(nm.Name)
					}
				}
			}
		}
	}
}
