package analyzer

import (
	"fmt"
	"go/ast"
	"strings"
)

// ValidateRename checks if renaming a symbol is safe
func (v *ExportedRenameValidator) ValidateRename(oldName, newName string) (*RenameValidation, error) {
	validation := &RenameValidation{
		IsExported: isExportedName(oldName),
		Warnings:   make([]string, 0),
	}

	// Check if new name is valid Go identifier
	if !isValidIdentifier(newName) {
		validation.SafeToRename = false
		validation.Warnings = append(validation.Warnings, fmt.Sprintf("Invalid identifier: %s", newName))
		return validation, nil
	}

	// Check if new name conflicts with built-ins
	if isBuiltinName(newName) {
		validation.SafeToRename = false
		validation.Warnings = append(validation.Warnings, fmt.Sprintf("Conflicts with builtin: %s", newName))
		return validation, nil
	}

	// Find all occurrences of the symbol
	var targetFile string
	var targetLine int
	refs := v.findSymbolReferences(oldName)

	if len(refs) == 0 {
		validation.SafeToRename = false
		validation.Warnings = append(validation.Warnings, fmt.Sprintf("Symbol not found: %s", oldName))
		return validation, nil
	}

	// Categorize references
	for _, ref := range refs {
		if ref.Type == "definition" {
			targetFile = ref.File
			targetLine = ref.Line
		}
		if strings.HasSuffix(ref.File, "_test.go") {
			validation.TestReferences++
		} else {
			validation.InternalReferences++
		}
	}

	validation.TargetFile = targetFile
	validation.TargetLine = targetLine

	// For exported symbols, warn if there are external references
	// (This is conservative; actual external package detection requires more context)
	if validation.IsExported && validation.InternalReferences > 0 {
		validation.CanRenameInPackage = true
		validation.Warnings = append(validation.Warnings,
			"Symbol is exported; external packages may reference it")
	}

	// Collect referring symbols
	validation.ReferringSymbols = v.getReferringSymbols(oldName)

	// Safe to rename if:
	// - Symbol exists
	// - New name is valid
	// - No conflicts
	validation.SafeToRename = len(validation.Warnings) == 0 && len(refs) > 0
	validation.CanRenameInPackage = true

	return validation, nil
}

// findSymbolReferences finds all references to a symbol
func (v *ExportedRenameValidator) findSymbolReferences(symbolName string) []*SymbolUse {
	var uses []*SymbolUse

	for i, f := range v.files {
		filePath := v.filePaths[i]

		ast.Inspect(f, func(node ast.Node) bool {
			switch n := node.(type) {
			case *ast.FuncDecl:
				if n.Name.Name == symbolName {
					uses = append(uses, &SymbolUse{
						File:       filePath,
						Line:       v.fset.Position(n.Pos()).Line,
						SymbolName: symbolName,
						Type:       TypeFunction,
						Context:    "define",
					})
				}
			case *ast.Ident:
				if n.Name == symbolName && isDefinition(n) {
					uses = append(uses, &SymbolUse{
						File:       filePath,
						Line:       v.fset.Position(n.Pos()).Line,
						SymbolName: symbolName,
						Context:    "define",
					})
				} else if n.Name == symbolName {
					uses = append(uses, &SymbolUse{
						File:       filePath,
						Line:       v.fset.Position(n.Pos()).Line,
						SymbolName: symbolName,
						Context:    "read",
					})
				}
			}
			return true
		})
	}

	return uses
}

// getReferringSymbols returns functions/methods that reference a symbol
func (v *ExportedRenameValidator) getReferringSymbols(symbolName string) []string {
	seen := make(map[string]bool)
	var symbols []string

	for _, f := range v.files {
		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if v.funcReferencesSymbol(d, symbolName) {
					if !seen[d.Name.Name] {
						seen[d.Name.Name] = true
						symbols = append(symbols, d.Name.Name)
					}
				}
			}
		}
	}

	return symbols
}

// funcReferencesSymbol checks if a function references a symbol
func (v *ExportedRenameValidator) funcReferencesSymbol(fn *ast.FuncDecl, symbolName string) bool {
	found := false
	ast.Inspect(fn, func(node ast.Node) bool {
		if ident, ok := node.(*ast.Ident); ok {
			if ident.Name == symbolName && !isDefinition(ident) {
				found = true
				return false
			}
		}
		return true
	})
	return found
}
