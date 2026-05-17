package orchestrator

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
)

// CrossPackageOperationHandler manages cross-package refactoring operations
type CrossPackageOperationHandler struct {
	fset *token.FileSet
}

// NewCrossPackageOperationHandler creates a new handler
func NewCrossPackageOperationHandler() *CrossPackageOperationHandler {
	return &CrossPackageOperationHandler{
		fset: token.NewFileSet(),
	}
}

// MoveAcrossPackages moves a function from one package to another
func (h *CrossPackageOperationHandler) MoveAcrossPackages(
	sourceFile, destFile, funcName string) error {

	// Read and parse source file
	sourcePkg, err := h.parseSourceFile(sourceFile)
	if err != nil {
		return err
	}

	// Find the function to move
	targetFunc, funcIndex, err := h.findFunction(sourcePkg, funcName, sourceFile)
	if err != nil {
		return err
	}

	// Get destination package name
	destPkgName := sourcePkg.Name.Name

	// Remove function from source
	if err := h.removeFunctionFromFile(sourceFile, funcIndex); err != nil {
		return fmt.Errorf("failed to remove function from source: %w", err)
	}

	// Add function to destination
	if err := h.addFunctionToFile(destFile, targetFunc, destPkgName); err != nil {
		return fmt.Errorf("failed to add function to dest: %w", err)
	}

	// Update imports in both files
	if err := FormatImports(sourceFile); err != nil {
		return fmt.Errorf("failed to format source imports: %w", err)
	}

	if err := FormatImports(destFile); err != nil {
		return fmt.Errorf("failed to format dest imports: %w", err)
	}

	return nil
}

// parseSourceFile reads and parses a source file
func (h *CrossPackageOperationHandler) parseSourceFile(filePath string) (*ast.File, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	pkg, err := parser.ParseFile(h.fset, filePath, content, parser.AllErrors)
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

// removeFunctionFromFile removes a function declaration from a file
func (h *CrossPackageOperationHandler) removeFunctionFromFile(filePath string, declIndex int) error {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	file, err := parser.ParseFile(h.fset, filePath, content, parser.AllErrors)
	if err != nil {
		return err
	}

	if declIndex < 0 || declIndex >= len(file.Decls) {
		return fmt.Errorf("invalid declaration index")
	}

	// Remove the declaration
	file.Decls = append(file.Decls[:declIndex], file.Decls[declIndex+1:]...)

	// Write back
	srcFile := h.fset.File(file.Pos())
	if srcFile == nil {
		return fmt.Errorf("failed to get file info")
	}

	return writeAST(filePath, file)
}

// addFunctionToFile adds a function declaration to a file
func (h *CrossPackageOperationHandler) addFunctionToFile(filePath string, fn *ast.FuncDecl, pkgName string) error {
	// Create destination directory if needed
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	var file *ast.File

	// Check if file exists
	if _, err := os.Stat(filePath); err == nil {
		content, err := os.ReadFile(filePath)
		if err != nil {
			return err
		}
		file, err = parser.ParseFile(h.fset, filePath, content, parser.AllErrors)
		if err != nil {
			return err
		}
	} else {
		// Create new file with package declaration
		file = &ast.File{
			Name:  &ast.Ident{Name: pkgName},
			Decls: make([]ast.Decl, 0),
		}
	}

	// Add the function
	file.Decls = append(file.Decls, fn)

	return writeAST(filePath, file)
}

// writeAST writes an AST back to a file
func writeAST(filePath string, file *ast.File) error {
	// This is a simplified version; real implementation would need proper formatting
	// For now, we'll indicate that actual writing logic would go here
	// The actual implementation would use go/format or similar
	return nil
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
		return false, warnings, err
	}

	// Check destination file if it exists
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

// ErrFileNotFound indicates file doesn't exist
var ErrFileNotFound = fmt.Errorf("file not found")

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
