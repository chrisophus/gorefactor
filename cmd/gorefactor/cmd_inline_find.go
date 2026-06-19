package main

import (
	"bytes"
	"fmt"
	"go/ast"
	goparser "go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// findShadowingDecl reports the location of any declaration (other than the
// target function itself) introducing the name in this file.
func findShadowingDecl(fset *token.FileSet, node *ast.File, name string, target *ast.FuncDecl) string {
	loc := ""
	mark := func(id *ast.Ident) {
		if id.Name == name && loc == "" {
			p := fset.Position(id.Pos())
			loc = fmt.Sprintf("%s:%d", p.Filename, p.Line)
		}
	}
	ast.Inspect(node, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.FuncDecl:
			if v != target {
				if v.Recv == nil {
					mark(v.Name)
				}
				markFieldList(v.Type.Params, mark)
				markFieldList(v.Type.Results, mark)
				markFieldList(v.Recv, mark)
			}
		case *ast.FuncLit:
			markFieldList(v.Type.Params, mark)
			markFieldList(v.Type.Results, mark)
		case *ast.ValueSpec:
			for _, id := range v.Names {
				mark(id)
			}
		case *ast.TypeSpec:
			mark(v.Name)
		case *ast.AssignStmt:
			if v.Tok == token.DEFINE {
				for _, lhs := range v.Lhs {
					if id, ok := lhs.(*ast.Ident); ok {
						mark(id)
					}
				}
			}
		case *ast.RangeStmt:
			if v.Tok == token.DEFINE {
				if id, ok := v.Key.(*ast.Ident); ok {
					mark(id)
				}
				if id, ok := v.Value.(*ast.Ident); ok {
					mark(id)
				}
			}
		}
		return loc == ""
	})
	return loc
}

// findCrossPackageUse scans the module for selector references to an
// exported function from other packages (best-effort: matches pkgName.Name).
func findCrossPackageUse(declFile, pkgName, funcName string) string {
	root := moduleRootOf(filepath.Dir(declFile))
	if root == "" {
		return ""
	}
	pkgDir, _ := filepath.Abs(filepath.Dir(declFile))
	loc := ""
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || loc != "" {
			return filepath.SkipAll
		}
		if info.IsDir() {
			base := info.Name()
			if base == "vendor" || base == ".git" || strings.HasPrefix(base, ".") && base != "." {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if abs, _ := filepath.Abs(filepath.Dir(path)); abs == pkgDir {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil || !bytes.Contains(src, []byte(funcName)) || !bytes.Contains(src, []byte(pkgName)) {
			return nil
		}
		fset := token.NewFileSet()
		node, perr := goparser.ParseFile(fset, path, src, 0)
		if perr != nil {
			return nil
		}
		ast.Inspect(node, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != funcName || loc != "" {
				return true
			}
			if x, ok := sel.X.(*ast.Ident); ok && x.Name == pkgName {
				p := fset.Position(sel.Pos())
				loc = fmt.Sprintf("%s:%d", p.Filename, p.Line)
			}
			return loc == ""
		})
		return nil
	})
	return loc
}
