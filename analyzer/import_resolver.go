package analyzer

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// ImportResolver handles import resolution when moving code between files
type ImportResolver struct {
	fset *token.FileSet
}

// NewImportResolver creates a new import resolver
func NewImportResolver() *ImportResolver {
	return &ImportResolver{
		fset: token.NewFileSet(),
	}
}

// ResolveImportsForMove determines what imports are needed when moving a function
func (ir *ImportResolver) ResolveImportsForMove(
	srcFile, destFile, funcName string) ([]ImportChange, error) {

	// Parse source file and find function
	srcPkg, targetFunc, err := ir.findFunctionInFile(srcFile, funcName)
	if err != nil {
		return nil, fmt.Errorf(

			// Get imports used in the function
			"find function in file: %w", err)
	}

	usedImports := ir.findImportsUsedInFunc(srcPkg, targetFunc)

	// Get existing imports in destination
	destImports := ir.getExistingImports(destFile)

	// Generate import changes for missing imports
	var changes []ImportChange
	for impPath := range usedImports {
		if !destImports[impPath] {
			changes = append(changes, ImportChange{
				Type:       "add",
				ImportPath: impPath,
				File:       destFile,
				Reason:     fmt.Sprintf("Used by function %s", funcName),
			})
		}
	}

	return changes, nil
}

// NeedToRemoveImport checks if an import is no longer needed in a file
func (ir *ImportResolver) NeedToRemoveImport(filePath, impPath string) (bool, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return false, fmt.Errorf("read file: %w", err)
	}

	pkg, err := parser.ParseFile(ir.fset, filePath, content, parser.AllErrors)
	if err != nil {
		return false, fmt.Errorf(

			// Extract package name from import path
			"parse file: %w", err)
	}

	pkgName := filepath.Base(impPath)
	for _, imp := range pkg.Imports {
		if strings.Trim(imp.Path.Value, "\"") == impPath {
			if imp.Name != nil {
				pkgName = imp.Name.Name
			}
			break
		}
	}

	// Check if this package name is used anywhere in the file
	isUsed := false
	ast.Inspect(pkg, func(node ast.Node) bool {
		if ident, ok := node.(*ast.Ident); ok {
			if ident.Name == pkgName {
				isUsed = true
				return false
			}
		}
		return true
	})

	return !isUsed, nil
}

// ApplyImportChanges applies a list of import changes to files
func (ir *ImportResolver) ApplyImportChanges(changes []ImportChange) error {
	for _, change := range changes {
		switch change.Type {
		case "add":
			if err := ir.addImport(change.File, change.ImportPath); err != nil {
				return fmt.Errorf("failed to add import %s to %s: %w",
					change.ImportPath, change.File, err)
			}
		case "remove":
			if err := ir.removeImport(change.File, change.ImportPath); err != nil {
				return fmt.Errorf("failed to remove import %s from %s: %w",
					change.ImportPath, change.File, err)
			}
		}
	}
	return nil
}

// findFunctionInFile locates a function in a source file
func (ir *ImportResolver) findFunctionInFile(filePath, funcName string) (*ast.File, *ast.FuncDecl, error) {
	srcContent, err := os.ReadFile(filePath)
	if err != nil {
		return nil, nil, err
	}

	srcPkg, err := parser.ParseFile(ir.fset, filePath, srcContent, parser.AllErrors)
	if err != nil {
		return nil, nil, err
	}

	for _, decl := range srcPkg.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == funcName {
			return srcPkg, fn, nil
		}
	}

	return nil, nil, fmt.Errorf("function not found: %s", funcName)
}

// getExistingImports reads imports from a file
func (ir *ImportResolver) getExistingImports(filePath string) map[string]bool {
	imports := make(map[string]bool)

	if _, err := os.Stat(filePath); err != nil {
		return imports
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return imports
	}

	pkg, err := parser.ParseFile(ir.fset, filePath, content, parser.AllErrors)
	if err != nil {
		return imports
	}

	for _, imp := range pkg.Imports {
		impPath := strings.Trim(imp.Path.Value, "\"")
		imports[impPath] = true
	}

	return imports
}

// findImportsUsedInFunc identifies which imports are used within a function
func (ir *ImportResolver) findImportsUsedInFunc(pkg *ast.File, fn *ast.FuncDecl) map[string]bool {
	used := make(map[string]bool)
	usedPackages := make(map[string]bool)

	// Walk the function body to find external package references
	ast.Inspect(fn, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.SelectorExpr:
			// foo.Bar -> foo is a package reference
			if ident, ok := n.X.(*ast.Ident); ok {
				usedPackages[ident.Name] = true
			}
		}
		return true
	})

	// Map package names to import paths
	pkgNameToPath := make(map[string]string)
	for _, imp := range pkg.Imports {
		impPath := strings.Trim(imp.Path.Value, "\"")
		// Extract last component as default package name
		pkgName := filepath.Base(impPath)
		if imp.Name != nil {
			pkgName = imp.Name.Name
		}
		pkgNameToPath[pkgName] = impPath
	}

	// Resolve used packages to import paths
	for pkgName := range usedPackages {
		if impPath, ok := pkgNameToPath[pkgName]; ok {
			used[impPath] = true
		}
	}

	return used
}

// addImport adds an import to a file
func (ir *ImportResolver) addImport(filePath, impPath string) error {
	// This would typically use go/ast and go/format to properly add the import
	// For now, this is a placeholder that indicates where the logic would go
	return nil
}

// removeImport removes an import from a file
func (ir *ImportResolver) removeImport(filePath, impPath string) error {
	// This would typically use go/ast and go/format to properly remove the import
	// For now, this is a placeholder that indicates where the logic would go
	return nil
}

// ImportChange represents a needed import addition or removal
type ImportChange struct {
	Type       string // "add" or "remove"
	ImportPath string
	File       string
	Reason     string
}
