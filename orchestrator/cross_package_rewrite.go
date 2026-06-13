package orchestrator

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/tools/go/ast/astutil"
)

// apply performs the cross-package move after planning and checks succeeded:
// remove from source, qualify and append to destination, rewrite call sites,
// fix imports everywhere.
func (mv *crossPackageMove) apply() error {
	src, err := os.ReadFile(mv.sourceFile)
	if err != nil {
		return fmt.Errorf("failed to read source file: %w", err)
	}
	startLine := mv.fset.Position(mv.fn.Pos()).Line
	endLine := mv.fset.Position(mv.fn.End()).Line

	// Remove the declaration (and its comments) from the source file.
	declCode, remaining := extractDeclWithComments(src, mv.fset, mv.srcNode, mv.fn)
	declIndex := -1
	for i, d := range mv.srcNode.Decls {
		if d == ast.Decl(mv.fn) {
			declIndex = i
			break
		}
	}
	if declIndex < 0 {
		return fmt.Errorf("internal error: declaration %s not found in parsed source", mv.funcName)
	}
	mv.srcNode.Decls = append(mv.srcNode.Decls[:declIndex], mv.srcNode.Decls[declIndex+1:]...)
	mv.srcNode.Comments = remaining
	if err := writeFileAndImport(mv.sourceFile, mv.srcNode, mv.fset); err != nil {
		return err
	}

	// Qualify references the moved code keeps into the source package.
	if len(mv.exportedRefs) > 0 {
		declCode, err = qualifyDeclCode(declCode, mv.srcPkgName, mv.exportedRefs)
		if err != nil {
			return fmt.Errorf("failed to qualify moved code: %w", err)
		}
	}

	// Append to the destination with the destination's package name.
	if err := appendDeclToFile(mv.destFile, declCode, mv.destPkgName); err != nil {
		return err
	}
	if len(mv.exportedRefs) > 0 {
		if err := addImportToFile(mv.destFile, mv.srcPkgName, mv.srcImport); err != nil {
			return err
		}
	}

	rewritten, err := mv.rewriteCallSites()
	if err != nil {
		return err
	}

	mv.report = &CrossPackageMoveReport{
		SourceFile:       mv.sourceFile,
		DestFile:         mv.destFile,
		FuncName:         mv.funcName,
		SourcePackage:    mv.srcPkgName,
		DestPackage:      mv.destPkgName,
		SourceImportPath: mv.srcImport,
		DestImportPath:   mv.destImport,
		QualifiedSymbols: mv.exportedRefs,
		RewrittenFiles:   rewritten,
		SourceStartLine:  startLine,
		SourceEndLine:    endLine,
		DeclCode:         declCode,
	}
	return nil
}

// rewriteCallSites updates every known call site to reference the function in
// its new package and returns the list of files rewritten.
func (mv *crossPackageMove) rewriteCallSites() ([]string, error) {
	var rewritten []string
	for _, path := range uniqueFiles(mv.samePkgSites) {
		changed, err := rewriteBareReferences(path, mv.funcName, mv.destPkgName, mv.destImport)
		if err != nil {
			return rewritten, fmt.Errorf("failed to rewrite call sites in %s: %w", path, err)
		}
		if changed {
			rewritten = append(rewritten, path)
		}
	}
	for _, path := range uniqueFiles(mv.externalSites) {
		changed, err := mv.rewriteQualifiedReferences(path)
		if err != nil {
			return rewritten, fmt.Errorf("failed to rewrite call sites in %s: %w", path, err)
		}
		if changed {
			rewritten = append(rewritten, path)
		}
	}
	return rewritten, nil
}

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

// rewriteQualifiedReferences rewrites srcAlias.Func selectors in an external
// file to point at the destination package (or to a bare reference when the
// file already lives in the destination package).
func (mv *crossPackageMove) rewriteQualifiedReferences(path string) (bool, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return false, err
	}
	srcAlias := importAlias(node, mv.srcImport, mv.srcPkgName)
	destAlias := importAlias(node, mv.destImport, mv.destPkgName)

	fileDir, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return false, err
	}
	inDestPackage := fileDir == mv.destDir && node.Name.Name == mv.destPkgName

	changed := false
	astutil.Apply(node, func(c *astutil.Cursor) bool {
		sel, ok := c.Node().(*ast.SelectorExpr)
		if !ok {
			return true
		}
		x, ok := sel.X.(*ast.Ident)
		if !ok || x.Name != srcAlias || x.Obj != nil || sel.Sel.Name != mv.funcName {
			return true
		}
		if inDestPackage {
			c.Replace(ast.NewIdent(mv.funcName))
		} else {
			x.Name = destAlias
		}
		changed = true
		return true
	}, nil)
	if !changed {
		return false, nil
	}
	if !inDestPackage {
		addImportSpec(fset, node, mv.destPkgName, mv.destImport)
	}
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

// nonReferenceIdents collects identifiers that are not plain references:
// declaration names, selector members, struct-literal keys, field names,
// labels, and import aliases. Renaming passes must leave these alone.
func nonReferenceIdents(root ast.Node) map[*ast.Ident]bool {
	skip := map[*ast.Ident]bool{}
	ast.Inspect(root, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.SelectorExpr:
			skip[x.Sel] = true
		case *ast.KeyValueExpr:
			// Likely a struct-literal field name. (Map-literal keys that are
			// identifiers are mistakenly skipped too; acceptable heuristic
			// without type information.)
			if id, ok := x.Key.(*ast.Ident); ok {
				skip[id] = true
			}
		case *ast.Field:
			for _, nm := range x.Names {
				skip[nm] = true
			}
		case *ast.FuncDecl:
			skip[x.Name] = true
		case *ast.TypeSpec:
			skip[x.Name] = true
		case *ast.ValueSpec:
			for _, nm := range x.Names {
				skip[nm] = true
			}
		case *ast.AssignStmt:
			if x.Tok == token.DEFINE {
				for _, l := range x.Lhs {
					if id, ok := l.(*ast.Ident); ok {
						skip[id] = true
					}
				}
			}
		case *ast.LabeledStmt:
			skip[x.Label] = true
		case *ast.BranchStmt:
			if x.Label != nil {
				skip[x.Label] = true
			}
		case *ast.ImportSpec:
			if x.Name != nil {
				skip[x.Name] = true
			}
		}
		return true
	})
	return skip
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
	return symbols, nil
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

func fileImports(node *ast.File, importPath string) bool {
	for _, imp := range node.Imports {
		if p, err := strconv.Unquote(imp.Path.Value); err == nil && p == importPath {
			return true
		}
	}
	return false
}
