package orchestrator

import (
	"fmt"
	"go/ast"
	"go/parser"
	"os"
	"path/filepath"
)

// MoveAcrossPackages moves a top-level function from one package to another,
// qualifying references it keeps into the source package, rewriting call
// sites in the source package and across the module, and fixing imports on
// every touched file. It fails loudly — with the affected call sites listed —
// whenever the move would break the build.
func (h *CrossPackageOperationHandler) MoveAcrossPackages(sourceFile, destFile, funcName string) error {
	_, err := h.moveAcrossPackages(sourceFile, destFile, funcName)
	return err
}

// moveAcrossPackages implements MoveAcrossPackages and returns a report.
func (h *CrossPackageOperationHandler) moveAcrossPackages(sourceFile, destFile, funcName string) (*CrossPackageMoveReport, error) {
	mv, err := h.planCrossPackageMove(sourceFile, destFile, funcName)
	if err != nil {
		return nil, err
	}
	if err := mv.check(); err != nil {
		return nil, err
	}
	if err := mv.apply(); err != nil {
		return nil, err
	}
	return mv.report, nil
}

// planCrossPackageMove gathers everything needed to perform and validate the
// move without mutating any file.
func (h *CrossPackageOperationHandler) planCrossPackageMove(sourceFile, destFile, funcName string) (*crossPackageMove, error) {
	srcNode, err := h.parseSourceFile(sourceFile)
	if err != nil {
		return nil, err
	}
	fn, _, err := h.findFunction(srcNode, funcName, sourceFile)
	if err != nil {
		return nil, err
	}
	if fn.Recv != nil {
		return nil, fmt.Errorf(
			"cannot move method %s:%s across packages: cross-package moves support only top-level functions; move the receiver type or convert the method to a function first",
			receiverTypeName(fn), funcName)
	}

	srcDir, err := filepath.Abs(filepath.Dir(sourceFile))
	if err != nil {
		return nil, err
	}
	destDir, err := filepath.Abs(filepath.Dir(destFile))
	if err != nil {
		return nil, err
	}

	mv := &crossPackageMove{
		fset:       h.fset,
		sourceFile: sourceFile,
		destFile:   destFile,
		funcName:   funcName,
		srcDir:     srcDir,
		destDir:    destDir,
		srcNode:    srcNode,
		fn:         fn,
		srcPkgName: srcNode.Name.Name,
	}

	mv.destPkgName, err = detectPackageName(destFile)
	if err != nil {
		return nil, err
	}
	if mv.destPkgName == mv.srcPkgName && srcDir == destDir {
		return nil, fmt.Errorf("destination %s is in the same package as %s; use a plain move", destFile, sourceFile)
	}

	modPath, modRoot, err := findModuleInfo(srcDir)
	if err != nil {
		return nil, fmt.Errorf("cross-package move requires a Go module: %w", err)
	}
	mv.srcImport, err = importPathFor(modPath, modRoot, srcDir)
	if err != nil {
		return nil, err
	}
	mv.destImport, err = importPathFor(modPath, modRoot, destDir)
	if err != nil {
		return nil, err
	}

	if err := mv.analyzeMovedDeclRefs(); err != nil {
		return nil, err
	}
	if err := mv.findCallSites(modRoot); err != nil {
		return nil, err
	}
	mv.srcImportsDest = dirImports(mv.fset, srcDir, mv.srcPkgName, mv.destImport)
	mv.destImportsSrc = dirImports(mv.fset, destDir, mv.destPkgName, mv.srcImport)
	return mv, nil
}

// CanMoveSafely checks if a function can be safely moved to another package
func (h *CrossPackageOperationHandler) CanMoveSafely(
	sourceFile, destFile, funcName string) (bool, []string, error) {

	warnings := []string{}

	// Check if function is exported
	if len(funcName) > 0 && funcName[0] >= 'A' && funcName[0] <= 'Z' {
		warnings = append(warnings, "Function is exported; external packages may reference it")
	}

	// Parse source file
	sourcePkg, err := h.parseSourceFile(sourceFile)
	if err != nil {
		return false, warnings, fmt.Errorf(

			// Check destination file if it exists
			"parse source file: %w", err)
	}

	destPkg, destErr := h.parseDestinationFile(destFile)
	if destErr != nil && destErr != ErrFileNotFound {
		return false, warnings, destErr
	}

	// If destination exists, verify packages match
	if destPkg != nil && sourcePkg.Name.Name != destPkg.Name.Name {
		return false, append(warnings, "Target file is in a different package"), nil
	}

	return true, warnings, nil
}

// parseSourceFile reads and parses a source file
func (h *CrossPackageOperationHandler) parseSourceFile(filePath string) (*ast.File, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	pkg, err := parser.ParseFile(h.fset, filePath, content, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("failed to parse file: %w", err)
	}

	return pkg, nil
}

// findFunction locates a function by name in a parsed file
func (h *CrossPackageOperationHandler) findFunction(
	pkg *ast.File, funcName, filePath string) (*ast.FuncDecl, int, error) {
	for i, decl := range pkg.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == funcName {
			return fn, i, nil
		}
	}
	return nil, -1, fmt.Errorf("function %s not found in %s", funcName, filePath)
}

// parseDestinationFile parses a destination file, returning ErrFileNotFound if it doesn't exist
func (h *CrossPackageOperationHandler) parseDestinationFile(filePath string) (*ast.File, error) {
	_, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrFileNotFound
		}
		return nil, err
	}

	return h.parseSourceFile(filePath)
}
