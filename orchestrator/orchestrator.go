package orchestrator

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
)

// NewOrchestrator creates a new orchestrator instance
func NewOrchestrator() *Orchestrator {
	return &Orchestrator{
		plans: make(map[string]*RefactoringPlan),
	}
}

// executeOperation executes a single refactoring operation

// Check conditions first

// Special handling for insert_code with at_beginning on new files

// Check if file exists

// Skip target finding for new file creation

// Find the target using resilient targeting
// Note: insert_code operations may not need a target, but we'll still try to find one if specified

// For insert_code and rename_declaration, target is optional

// Try fallback strategy

// For insert_code, we can proceed without a target

// isCrossPackageMove returns true when the destination file is in a different
// package than the source file. It checks by comparing absolute directory
// paths; if either file cannot be stat'd or parsed the comparison falls back
// to directory inequality only.
func isCrossPackageMove(sourceFile, destFile string) bool {
	srcAbs, err1 := filepath.Abs(filepath.Dir(sourceFile))
	dstAbs, err2 := filepath.Abs(filepath.Dir(destFile))
	if err1 != nil || err2 != nil {
		return false
	}
	if srcAbs == dstAbs {
		return false // same directory → same package
	}
	// Different directories: check whether the destination package name
	// differs from the source package name (a destination in a sibling
	// directory that keeps the same package name is still a cross-pkg move).
	srcPkg := filePackageName(sourceFile)
	dstPkg := detectDestPackageName(destFile)
	if srcPkg == "" || dstPkg == "" {
		// Cannot determine package names — treat as cross-package when dirs differ.
		return true
	}
	return srcPkg != dstPkg
}

// filePackageName returns the package name declared in a Go source file, or
// empty string on error.
func filePackageName(path string) string {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, path, nil, parser.PackageClauseOnly)
	if err != nil || node == nil || node.Name == nil {
		return ""
	}
	return node.Name.Name
}

// detectDestPackageName returns the package name for a destination file.
// If the file does not exist yet it falls back to sibling files in the
// destination directory, then to a name derived from the directory.
func detectDestPackageName(destFile string) string {
	fset := token.NewFileSet()
	if _, err := os.Stat(destFile); err == nil {
		node, err := parser.ParseFile(fset, destFile, nil, parser.PackageClauseOnly)
		if err == nil && node != nil && node.Name != nil {
			return node.Name.Name
		}
	}
	// Destination file does not exist yet — look at siblings.
	dir := filepath.Dir(destFile)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".go" {
			continue
		}
		full := filepath.Join(dir, e.Name())
		node, err := parser.ParseFile(fset, full, nil, parser.PackageClauseOnly)
		if err == nil && node != nil && node.Name != nil && node.Name.Name != "" {
			return node.Name.Name
		}
	}
	return sanitizePackageName(filepath.Base(dir))
}
