package orchestrator

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"

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
		return fmt.Errorf(

			// Qualify references the moved code keeps into the source package.
			"write file and import: %w", err)
	}

	if len(mv.exportedRefs) > 0 {
		declCode, err = qualifyDeclCode(declCode, mv.srcPkgName, mv.exportedRefs)
		if err != nil {
			return fmt.Errorf("failed to qualify moved code: %w", err)
		}
	}

	// Append to the destination with the destination's package name.
	if err := appendDeclToFile(mv.destFile, declCode, mv.destPkgName); err != nil {
		return fmt.Errorf("append decl to file: %w", err)
	}
	if len(mv.exportedRefs) > 0 {
		if err := addImportToFile(mv.destFile, mv.srcPkgName, mv.srcImport); err != nil {
			return fmt.Errorf("add import to file: %w", err)
		}
	}

	rewritten, err := mv.rewriteCallSites()
	if err != nil {
		return fmt.Errorf("rewrite call sites: %w", err)
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

// rewriteQualifiedReferences rewrites srcAlias.Func selectors in an external
// file to point at the destination package (or to a bare reference when the
// file already lives in the destination package).
func (mv *crossPackageMove) rewriteQualifiedReferences(path string) (bool, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return false, fmt.Errorf("parse file: %w", err)
	}
	srcAlias := importAlias(node, mv.srcImport, mv.srcPkgName)
	destAlias := importAlias(node, mv.destImport, mv.destPkgName)

	fileDir, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return false, fmt.Errorf("abs: %w", err)
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
