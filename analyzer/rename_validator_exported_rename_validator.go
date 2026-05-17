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

	if !v.validateIdentifierAndBuiltins(newName, validation) {
		return validation, nil
	}

	refs := v.findSymbolReferences(oldName)
	if len(refs) == 0 {
		validation.SafeToRename = false
		validation.Warnings = append(validation.Warnings, fmt.Sprintf("Symbol not found: %s", oldName))
		return validation, nil
	}

	targetFile := v.categorizeReferences(refs, validation)
	validation.TargetFile = targetFile

	v.warnIfExportedSymbol(validation)
	validation.ReferringSymbols = v.getReferringSymbols(oldName)

	validation.SafeToRename = len(validation.Warnings) == 0 && len(refs) > 0
	validation.CanRenameInPackage = true

	return validation, nil
}

func (v *ExportedRenameValidator) validateIdentifierAndBuiltins(newName string, validation *RenameValidation) bool {
	if !isValidIdentifier(newName) {
		validation.SafeToRename = false
		validation.Warnings = append(validation.Warnings, fmt.Sprintf("Invalid identifier: %s", newName))
		return false
	}

	if isBuiltinName(newName) {
		validation.SafeToRename = false
		validation.Warnings = append(validation.Warnings, fmt.Sprintf("Conflicts with builtin: %s", newName))
		return false
	}

	return true
}

func (v *ExportedRenameValidator) categorizeReferences(refs []*SymbolUse, validation *RenameValidation) string {
	var targetFile string

	for _, ref := range refs {
		if ref.Type == TypeFunction {
			targetFile = ref.File
			validation.TargetLine = ref.Line
		}
		if strings.HasSuffix(ref.File, "_test.go") {
			validation.TestReferences++
		} else {
			validation.InternalReferences++
		}
	}

	return targetFile
}

func (v *ExportedRenameValidator) warnIfExportedSymbol(validation *RenameValidation) {
	if validation.IsExported && validation.InternalReferences > 0 {
		validation.CanRenameInPackage = true
		validation.Warnings = append(validation.Warnings,
			"Symbol is exported; external packages may reference it")
	}
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
