package analyzer

import (
	"fmt"
	"go/ast"
	"strings"
)

// AdviseRename analyzes a proposed rename and returns advisory hints.
// Blocking hints are definite problems (invalid identifier, builtin or
// package-level collision, symbol not found); advisory hints are
// name-match-only observations. It never claims a rename is safe.
func (v *RenameAdvisor) AdviseRename(oldName, newName string) (*RenameHints, error) {
	hints := &RenameHints{
		IsExported: isExportedName(oldName),
	}

	// Definite problems first: these hold regardless of scope resolution.
	if !isValidIdentifier(newName) {
		hints.Blocking = append(hints.Blocking, fmt.Sprintf("invalid identifier: %s", newName))
		return hints, nil
	}
	if isBuiltinName(newName) {
		hints.Blocking = append(hints.Blocking, fmt.Sprintf("conflicts with builtin: %s", newName))
		return hints, nil
	}
	if v.packageLevelNames()[newName] {
		hints.Blocking = append(hints.Blocking, fmt.Sprintf("%s is already declared at package level in %s", newName, v.packageName))
		return hints, nil
	}

	// Find all occurrences of the symbol
	refs := v.findSymbolReferences(oldName)
	if len(refs) == 0 {
		hints.Blocking = append(hints.Blocking, fmt.Sprintf("symbol not found: %s", oldName))
		return hints, nil
	}

	// Categorize references
	for _, ref := range refs {
		if ref.Type == "definition" {
			hints.TargetFile = ref.File
			hints.TargetLine = ref.Line
		}
		if strings.HasSuffix(ref.File, "_test.go") {
			hints.TestReferences++
		} else {
			hints.InternalReferences++
		}
	}

	if hints.IsExported {
		hints.Advisory = append(hints.Advisory,
			"symbol is exported; packages outside this directory are invisible to this name-match-only analysis and may reference it")
	}
	hints.Advisory = append(hints.Advisory,
		"reference counts are textual matches, not scope-resolved: shadowed or same-named locals are counted too")

	hints.ReferringSymbols = v.getReferringSymbols(oldName)

	return hints, nil
}

// packageLevelNames returns every top-level declared name in the package —
// a rename onto one of them is a definite collision.
func (v *RenameAdvisor) packageLevelNames() map[string]bool {
	names := make(map[string]bool)
	for _, f := range v.files {
		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil {
					names[d.Name.Name] = true
				}
			case *ast.GenDecl:
				addGenDeclNames(d, names)
			}
		}
	}
	return names

}

func addGenDeclNames(d *ast.GenDecl, names map[string]bool) {
	for _, spec := range d.Specs {
		switch s := spec.(type) {
		case *ast.TypeSpec:
			names[s.Name.Name] = true
		case *ast.ValueSpec:
			for _, n := range s.Names {
				names[n.Name] = true
			}
		}
	}
}

// findSymbolReferences finds all references to a symbol
func (v *RenameAdvisor) findSymbolReferences(symbolName string) []*SymbolUse {
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
func (v *RenameAdvisor) getReferringSymbols(symbolName string) []string {
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
func (v *RenameAdvisor) funcReferencesSymbol(fn *ast.FuncDecl, symbolName string) bool {
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
