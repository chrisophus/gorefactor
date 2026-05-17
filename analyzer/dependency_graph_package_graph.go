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

// buildGraph recursively builds the dependency graph
func (pg *PackageGraph) buildGraph(dir string, pkgPrefix string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	goFiles, subdirs := pg.classifyEntries(entries, dir)

	// Parse this directory's packages
	if len(goFiles) > 0 {
		if err := pg.parsePackageDir(dir, goFiles, pkgPrefix); err != nil {
			return err
		}
	}

	// Recursively handle subdirectories
	return pg.processSubdirectories(dir, pkgPrefix, subdirs)
}

// classifyEntries separates directories from Go files
func (pg *PackageGraph) classifyEntries(entries []os.DirEntry, dir string) ([]string, []string) {
	var goFiles []string
	var subdirs []string

	for _, entry := range entries {
		if entry.IsDir() {
			if !strings.HasPrefix(entry.Name(), ".") && entry.Name() != "vendor" {
				subdirs = append(subdirs, entry.Name())
			}
		} else if strings.HasSuffix(entry.Name(), ".go") && !strings.HasSuffix(entry.Name(), "_test.go") {
			goFiles = append(goFiles, filepath.Join(dir, entry.Name()))
		}
	}

	return goFiles, subdirs
}

// processSubdirectories recursively processes subdirectories
func (pg *PackageGraph) processSubdirectories(baseDir, pkgPrefix string, subdirs []string) error {
	for _, subdir := range subdirs {
		subPath := filepath.Join(baseDir, subdir)
		newPrefix := subdir
		if pkgPrefix != "" {
			newPrefix = pkgPrefix + "/" + subdir
		}
		if err := pg.buildGraph(subPath, newPrefix); err != nil {
			return err
		}
	}
	return nil
}

func (pg *PackageGraph) extractPackageMetadata(pkg *ast.Package, pkgPath string, info *PackageInfo) {
	importsSet := make(map[string]bool)
	var funcCount, typeCount int

	for _, file := range pkg.Files {

		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				funcCount++
			case *ast.GenDecl:
				if d.Tok == token.TYPE {
					typeCount += len(d.Specs)
				}
			}
		}

		for _, imp := range file.Imports {
			impPath := strings.Trim(imp.Path.Value, "\"")
			if !importsSet[impPath] {
				importsSet[impPath] = true
				info.Imports = append(info.Imports, impPath)

				pg.edges[pkgPath] = append(pg.edges[pkgPath], &ImportEdge{
					From:   pkgPath,
					To:     impPath,
					Direct: true,
				})
			}
		}
	}

	info.Functions = funcCount
	info.Types = typeCount
}

// parsePackageDir parses Go files in a directory and extracts import information
func (pg *PackageGraph) parsePackageDir(dir string, files []string, pkgPath string) error {
	pkgs, err := parser.ParseDir(pg.fset, dir, nil, parser.ImportsOnly)
	if err != nil {
		return fmt.Errorf("failed to parse directory %s: %w", dir, err)
	}

	for pkgName, pkg := range pkgs {
		info := &PackageInfo{
			Path:    pkgPath,
			Dir:     dir,
			Name:    pkgName,
			Imports: make([]string, 0),
			Files:   len(pkg.Files),
		}

		pg.extractPackageMetadata(pkg, pkgPath, info)
		pg.packages[pkgPath] = info
	}

	return nil
}

// GetPackage returns info about a specific package
func (pg *PackageGraph) GetPackage(pkgPath string) *PackageInfo {
	return pg.packages[pkgPath]
}

// GetDependencies returns direct dependencies of a package
func (pg *PackageGraph) GetDependencies(pkgPath string) []*ImportEdge {
	return pg.edges[pkgPath]
}

// FindPath finds a dependency path between two packages
func (pg *PackageGraph) FindPath(from, to string) *DependencyPath {
	visited := make(map[string]bool)
	path := []string{from}

	if pg.findPathDFS(from, to, visited, &path) {
		return &DependencyPath{
			From:  from,
			To:    to,
			Path:  path,
			Depth: len(path) - 1,
		}
	}

	return nil
}

// findPathDFS uses depth-first search to find a path between packages
func (pg *PackageGraph) findPathDFS(current, target string, visited map[string]bool, path *[]string) bool {
	if current == target {
		return true
	}

	if visited[current] {
		return false
	}

	visited[current] = true

	for _, edge := range pg.edges[current] {
		*path = append(*path, edge.To)
		if pg.findPathDFS(edge.To, target, visited, path) {
			return true
		}
		*path = (*path)[:len(*path)-1]
	}

	return false
}

// HasCircularDependencies detects cycles in the dependency graph
func (pg *PackageGraph) HasCircularDependencies() [][]string {
	var cycles [][]string
	visited := make(map[string]bool)
	recStack := make(map[string]bool)

	for pkg := range pg.packages {
		if !visited[pkg] {
			var path []string
			if pg.hasCycleDFS(pkg, visited, recStack, &path) {
				cycles = append(cycles, path)
			}
		}
	}

	return cycles
}

// hasCycleDFS detects cycles using depth-first search with recursion stack
func (pg *PackageGraph) hasCycleDFS(pkg string, visited, recStack map[string]bool, path *[]string) bool {
	visited[pkg] = true
	recStack[pkg] = true
	*path = append(*path, pkg)

	for _, edge := range pg.edges[pkg] {
		if !visited[edge.To] {
			if pg.hasCycleDFS(edge.To, visited, recStack, path) {
				return true
			}
		} else if recStack[edge.To] {
			// Found a cycle
			return true
		}
	}

	*path = (*path)[:len(*path)-1]
	recStack[pkg] = false
	return false
}

// AllPackages returns all packages in the graph
func (pg *PackageGraph) AllPackages() []*PackageInfo {
	var pkgs []*PackageInfo
	for _, info := range pg.packages {
		pkgs = append(pkgs, info)
	}
	return pkgs
}

// Summary returns a string summary of the dependency graph
func (pg *PackageGraph) Summary() string {
	var result strings.Builder
	result.WriteString("=== Dependency Graph Summary ===\n")
	result.WriteString(fmt.Sprintf("Total packages: %d\n", len(pg.packages)))

	totalImports := 0
	for _, edges := range pg.edges {
		totalImports += len(edges)
	}
	result.WriteString(fmt.Sprintf("Total import edges: %d\n", totalImports))

	// Check for cycles
	cycles := pg.HasCircularDependencies()
	if len(cycles) > 0 {
		result.WriteString(fmt.Sprintf("Circular dependencies detected: %d\n", len(cycles)))
	} else {
		result.WriteString("No circular dependencies\n")
	}

	return result.String()
}

// PrintGraph outputs the graph in a human-readable format
func (pg *PackageGraph) PrintGraph() string {
	var result strings.Builder

	for _, pkg := range pg.AllPackages() {
		result.WriteString(fmt.Sprintf("\n%s (%s)\n", pkg.Name, pkg.Path))
		result.WriteString(fmt.Sprintf("  Dir: %s\n", pkg.Dir))
		result.WriteString(fmt.Sprintf("  Functions: %d, Types: %d, Files: %d\n", pkg.Functions, pkg.Types, pkg.Files))

		if len(pkg.Imports) > 0 {
			result.WriteString("  Imports:\n")
			for _, imp := range pkg.Imports {
				result.WriteString(fmt.Sprintf("    - %s\n", imp))
			}
		}
	}

	return result.String()
}
