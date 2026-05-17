package analyzer

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// ExportedRenameValidator checks if it's safe to rename an exported symbol within a package
type ExportedRenameValidator struct {
	packageName string
	fset        *token.FileSet
	files       []*ast.File
	filePaths   []string
}

// RenameValidation represents the validation result for a rename operation
type RenameValidation struct {
	IsExported         bool
	CanRenameInPackage bool
	ReferringSymbols   []string // Functions/methods that reference this symbol
	ExternalReferences int
	InternalReferences int
	TestReferences     int
	SafeToRename       bool
	Warnings           []string
	TargetFile         string
	TargetLine         int
}

// NewExportedRenameValidator creates a new validator for a package directory
func NewExportedRenameValidator(packageDir string) (*ExportedRenameValidator, error) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, packageDir, func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.AllErrors)
	if err != nil {
		return nil, fmt.Errorf("failed to parse package: %w", err)
	}

	if len(pkgs) == 0 {
		return nil, fmt.Errorf("no packages found in %s", packageDir)
	}

	var packageName string
	var files []*ast.File
	var filePaths []string

	for pname, pkg := range pkgs {
		if packageName == "" {
			packageName = pname
		}
		for fpath, f := range pkg.Files {
			files = append(files, f)
			filePaths = append(filePaths, fpath)
		}
	}

	return &ExportedRenameValidator{
		packageName: packageName,
		fset:        fset,
		files:       files,
		filePaths:   filePaths,
	}, nil
}

// isExportedName checks if a name is exported (starts with uppercase)
func isExportedName(name string) bool {
	if len(name) == 0 {
		return false
	}
	return unicode.IsUpper(rune(name[0]))
}

// isValidIdentifier checks if a string is a valid Go identifier
func isValidIdentifier(name string) bool {
	if len(name) == 0 {
		return false
	}
	if !isLetter(rune(name[0])) && name[0] != '_' {
		return false
	}
	for _, r := range name[1:] {
		if !isLetter(r) && !isDigit(r) && r != '_' {
			return false
		}
	}
	return true
}

// isLetter checks if a rune is a letter
func isLetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

// isDigit checks if a rune is a digit
func isDigit(r rune) bool {
	return r >= '0' && r <= '9'
}

// isBuiltinName checks if a name conflicts with Go builtins
func isBuiltinName(name string) bool {
	builtins := map[string]bool{
		"bool":       true,
		"byte":       true,
		"complex64":  true,
		"complex128": true,
		"error":      true,
		"float32":    true,
		"float64":    true,
		"int":        true,
		"int8":       true,
		"int16":      true,
		"int32":      true,
		"int64":      true,
		"rune":       true,
		"string":     true,
		"uint":       true,
		"uint8":      true,
		"uint16":     true,
		"uint32":     true,
		"uint64":     true,
		"uintptr":    true,
		"true":       true,
		"false":      true,
		"iota":       true,
		"nil":        true,
		"len":        true,
		"cap":        true,
		"make":       true,
		"new":        true,
		"append":     true,
		"copy":       true,
		"delete":     true,
		"complex":    true,
		"real":       true,
		"imag":       true,
		"panic":      true,
		"recover":    true,
		"print":      true,
		"println":    true,
	}
	return builtins[name]
}

// isDefinition checks if an identifier is being defined (approximate)
func isDefinition(ident *ast.Ident) bool {
	// This is a simplified check; real implementation would need parent node info
	return ident.Obj != nil && ident.Obj.Pos() == ident.Pos()
}

// SafetyReport generates a human-readable safety report
func (v *RenameValidation) SafetyReport(oldName, newName string) string {
	var report strings.Builder
	report.WriteString(fmt.Sprintf("Rename validation for: %s -> %s\n", oldName, newName))
	report.WriteString(fmt.Sprintf("Status: %v\n", v.SafeToRename))
	report.WriteString(fmt.Sprintf("Exported: %v\n", v.IsExported))

	if v.TargetFile != "" {
		report.WriteString(fmt.Sprintf("Location: %s:%d\n", filepath.Base(v.TargetFile), v.TargetLine))
	}

	report.WriteString(fmt.Sprintf("References: %d internal, %d test\n", v.InternalReferences, v.TestReferences))

	if len(v.ReferringSymbols) > 0 {
		report.WriteString(fmt.Sprintf("Referenced by: %v\n", v.ReferringSymbols))
	}

	if len(v.Warnings) > 0 {
		report.WriteString("Warnings:\n")
		for _, w := range v.Warnings {
			report.WriteString(fmt.Sprintf("  - %s\n", w))
		}
	}

	return report.String()
}
