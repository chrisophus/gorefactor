package orchestrator

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// findModuleInfo walks up from dir to the enclosing go.mod and returns the
// module path and module root directory.
func findModuleInfo(dir string) (modPath, modRoot string, err error) {
	d, err := filepath.Abs(dir)
	if err != nil {
		return "", "", err
	}
	for {
		data, rerr := os.ReadFile(filepath.Join(d, "go.mod"))
		if rerr == nil {
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "module") {
					mod := strings.TrimSpace(strings.TrimPrefix(line, "module"))
					mod = strings.Trim(mod, `"`)
					if mod != "" {
						return mod, d, nil
					}
				}
			}
			return "", "", fmt.Errorf("go.mod at %s has no module directive", d)
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", "", fmt.Errorf("no go.mod found above %s", dir)
		}
		d = parent
	}
}

// findQualifiedReferences scans the module for files (outside skipDir) that
// import srcImport and reference alias.funcName.
func findQualifiedReferences(fset *token.FileSet, modRoot, skipDir, srcImport, srcPkgName, funcName string) ([]CallSiteRef, error) {
	var sites []CallSiteRef
	err := filepath.WalkDir(modRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable entries are not this operation's problem
		}
		if d.IsDir() {
			name := d.Name()
			if path != modRoot && (strings.HasPrefix(name, ".") || name == "vendor" || name == "node_modules") {
				return filepath.SkipDir
			}
			if path != modRoot {
				if _, serr := os.Stat(filepath.Join(path, "go.mod")); serr == nil {
					return filepath.SkipDir // nested module
				}
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		abs, aerr := filepath.Abs(filepath.Dir(path))
		if aerr != nil || abs == skipDir {
			return nil
		}
		content, rerr := os.ReadFile(path)
		if rerr != nil || !bytes.Contains(content, []byte(srcImport)) || !bytes.Contains(content, []byte(funcName)) {
			return nil
		}
		node, perr := parser.ParseFile(fset, path, content, parser.ParseComments)
		if perr != nil {
			return nil
		}
		if !fileImports(node, srcImport) {
			return nil
		}
		alias := importAlias(node, srcImport, srcPkgName)
		ast.Inspect(node, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			x, ok := sel.X.(*ast.Ident)
			if !ok || x.Name != alias || x.Obj != nil || sel.Sel.Name != funcName {
				return true
			}
			sites = append(sites, CallSiteRef{
				File: path,
				Line: fset.Position(sel.Pos()).Line,
				Pkg:  node.Name.Name,
			})
			return true
		})
		return nil
	})
	return sites, err
}
