package orchestrator

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/tools/go/ast/astutil"
)

func uniqueFiles(sites []CallSiteRef) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range sites {
		if !seen[s.File] {
			seen[s.File] = true
			out = append(out, s.File)
		}
	}
	return out
}

// rewriteBareReferences rewrites unqualified references to funcName in a
// source-package file into pkgName.funcName and adds the needed import.
func rewriteBareReferences(path, funcName, pkgName, importPath string) (bool, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return false, err
	}
	skip := nonReferenceIdents(node)
	changed := false
	ast.Inspect(node, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		// The declaration is gone from the package by now, so a genuine
		// reference is unresolved (Obj == nil); resolved idents are shadows.
		if !ok || id.Name != funcName || skip[id] || id.Obj != nil {
			return true
		}
		id.Name = pkgName + "." + funcName
		changed = true
		return true
	})
	if !changed {
		return false, nil
	}
	addImportSpec(fset, node, pkgName, importPath)
	return true, writeFileAndImport(path, node, fset)
}

// qualifyDeclCode parses a standalone declaration and qualifies references to
// the given symbols with the package name, returning the rewritten code.
func qualifyDeclCode(declCode, pkgName string, symbols []string) (string, error) {
	want := map[string]bool{}
	for _, s := range symbols {
		want[s] = true
	}
	src := "package " + pkgName + "_decl\n\n" + declCode + "\n"
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, "decl.go", src, parser.ParseComments)
	if err != nil {
		return "", fmt.Errorf("moved declaration does not parse: %w", err)
	}
	skip := nonReferenceIdents(node)
	ast.Inspect(node, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		// In this standalone parse the package symbols are unresolved, so a
		// resolved ident is a local declaration or its use.
		if !ok || skip[id] || id.Obj != nil || !want[id.Name] {
			return true
		}
		id.Name = pkgName + "." + id.Name
		return true
	})
	var buf bytes.Buffer
	if err := format.Node(&buf, fset, node); err != nil {
		return "", err
	}
	out := buf.String()
	if idx := strings.Index(out, "\n"); idx >= 0 {
		out = out[idx+1:]
	}
	return strings.TrimSpace(out), nil
}

// addImportSpec adds an import to a parsed file, naming it explicitly when
// the package name differs from the import path's base.
func addImportSpec(fset *token.FileSet, node *ast.File, pkgName, importPath string) {
	if filepath.Base(importPath) == pkgName {
		astutil.AddImport(fset, node, importPath)
	} else {
		astutil.AddNamedImport(fset, node, pkgName, importPath)
	}
}

// addImportToFile parses a file from disk, adds an import, and writes it back.
func addImportToFile(path, pkgName, importPath string) error {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return err
	}
	addImportSpec(fset, node, pkgName, importPath)
	return writeFileAndImport(path, node, fset)
}

// importAlias returns the name by which importPath is referenced in a file:
// the explicit alias if one exists, otherwise the package's own name.
func importAlias(node *ast.File, importPath, pkgName string) string {
	for _, imp := range node.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil || p != importPath {
			continue
		}
		if imp.Name != nil {
			return imp.Name.Name
		}
		return pkgName
	}
	return pkgName
}

// extractDeclWithComments returns the source text of a declaration including
// its associated comments, plus the file's comment list with those comments
// removed (for writing the file back without them).
func extractDeclWithComments(src []byte, fset *token.FileSet, node *ast.File, decl ast.Decl) (string, []*ast.CommentGroup) {
	textStart := fset.Position(decl.Pos()).Offset
	textEnd := fset.Position(decl.End()).Offset
	var remaining []*ast.CommentGroup
	for _, cg := range node.Comments {
		if commentBelongsToDecl(fset, decl.Pos(), decl.End(), cg) {
			if off := fset.Position(cg.Pos()).Offset; off < textStart {
				textStart = off
			}
		} else {
			remaining = append(remaining, cg)
		}
	}
	return string(bytes.TrimSpace(src[textStart:textEnd])), remaining
}

// identResolvesWithin reports whether an identifier resolves to a declaration
// located inside the given node (i.e. it is local to it).
func identResolvesWithin(id *ast.Ident, node ast.Node) bool {
	if id.Obj == nil {
		return false
	}
	d, ok := id.Obj.Decl.(ast.Node)
	if !ok {
		return false
	}
	return d.Pos() >= node.Pos() && d.End() <= node.End()
}

// detectPackageName determines the package a destination file belongs to:
// the file's own package clause if it exists, otherwise the package of a
// sibling .go file, otherwise a name derived from the directory.
func detectPackageName(destFile string) (string, error) {
	fset := token.NewFileSet()
	if _, err := os.Stat(destFile); err == nil {
		node, err := parser.ParseFile(fset, destFile, nil, parser.PackageClauseOnly)
		if err != nil {
			return "", fmt.Errorf("cannot read package of %s: %w", destFile, err)
		}
		return node.Name.Name, nil
	}
	dir := filepath.Dir(destFile)
	files, err := packageGoFiles(dir)
	if err == nil {
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			node, err := parser.ParseFile(fset, f, nil, parser.PackageClauseOnly)
			if err != nil {
				continue
			}
			return node.Name.Name, nil
		}
	}
	name := sanitizePackageName(filepath.Base(dir))
	if name == "" {
		return "", fmt.Errorf("cannot derive a package name for %s; create the destination file with a package clause first", destFile)
	}
	return name, nil
}

// sanitizePackageName turns a directory name into a valid Go package name.
func sanitizePackageName(base string) string {
	var b strings.Builder
	for _, r := range base {
		if unicode.IsLetter(r) || r == '_' || (b.Len() > 0 && unicode.IsDigit(r)) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

// dirImports reports whether any file of the named package in dir imports
// importPath.
func dirImports(fset *token.FileSet, dir, pkgName, importPath string) bool {
	files, err := packageGoFiles(dir)
	if err != nil {
		return false
	}
	for _, path := range files {
		node, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil || node.Name.Name != pkgName {
			continue
		}
		for _, imp := range node.Imports {
			if p, err := strconv.Unquote(imp.Path.Value); err == nil && p == importPath {
				return true
			}
		}
	}
	return false
}

// importPathFor computes the import path of a directory inside a module.
func importPathFor(modPath, modRoot, dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(modRoot, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("%s is outside module %s", dir, modPath)
	}
	if rel == "." {
		return modPath, nil
	}
	return modPath + "/" + filepath.ToSlash(rel), nil
}

func fileImports(node *ast.File, importPath string) bool {
	for _, imp := range node.Imports {
		if p, err := strconv.Unquote(imp.Path.Value); err == nil && p == importPath {
			return true
		}
	}
	return false
}
