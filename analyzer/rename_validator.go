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

// RenameAdvisor produces advisory, name-match-only hints about renaming a
// symbol within a package. It deliberately does not promise safety: matching
// is textual (no go/types scope resolution), shadowing is not modeled, and
// packages outside the analyzed directory are invisible. Blocking hints mark
// conditions under which the rename is certainly wrong (invalid identifier,
// builtin or package-level collision, symbol not found); everything else is
// advisory. For a scope-aware exported rename, use gopls.
//
// (Harness-integrity plan item 7: this replaced ExportedRenameValidator,
// whose SafeToRename boolean promised a soundness the name-based analysis
// cannot deliver.)
type RenameAdvisor struct {
	packageName string
	fset        *token.FileSet
	files       []*ast.File
	filePaths   []string
}

// RenameHints is the advisory result of analyzing a proposed rename.
// Blocking hints are definite problems; Advisory hints are name-match-only
// observations the caller must judge. There is deliberately no "safe"
// boolean — absence of blocking hints means "nothing certainly wrong was
// found by a name-based scan", not "safe".
type RenameHints struct {
	IsExported         bool
	ReferringSymbols   []string // Functions/methods that reference this symbol
	InternalReferences int
	TestReferences     int
	Blocking           []string
	Advisory           []string
	TargetFile         string
	TargetLine         int
}

// HasBlocking reports whether any definite problem was found.
func (h *RenameHints) HasBlocking() bool { return len(h.Blocking) > 0 }

// NewRenameAdvisor creates a new advisor for a package directory
func NewRenameAdvisor(packageDir string) (*RenameAdvisor, error) {
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

	return &RenameAdvisor{
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

// Report generates a human-readable report of the hints. It states the
// analysis's limits explicitly instead of a safe/unsafe verdict.
func (h *RenameHints) Report(oldName, newName string) string {
	var report strings.Builder
	fmt.Fprintf(&report, "Rename hints for: %s -> %s (name-match only — this analysis cannot prove a rename safe)\n", oldName, newName)
	fmt.Fprintf(&report, "Exported: %v\n", h.IsExported)

	if h.TargetFile != "" {
		fmt.Fprintf(&report, "Location: %s:%d\n", filepath.Base(h.TargetFile), h.TargetLine)
	}

	fmt.Fprintf(&report, "References: %d internal, %d test\n", h.InternalReferences, h.TestReferences)

	if len(h.ReferringSymbols) > 0 {
		fmt.Fprintf(&report, "Referenced by: %v\n", h.ReferringSymbols)
	}

	if len(h.Blocking) > 0 {
		report.WriteString("Blocking:\n")
		for _, b := range h.Blocking {
			fmt.Fprintf(&report, "  - %s\n", b)
		}
	}
	if len(h.Advisory) > 0 {
		report.WriteString("Advisory:\n")
		for _, a := range h.Advisory {
			fmt.Fprintf(&report, "  - %s\n", a)
		}
	}

	return report.String()
}
