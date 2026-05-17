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

// ImportEdge represents an import relationship between packages
type ImportEdge struct {
	From     string // Source package path
	To       string // Target package path
	Direct   bool   // Whether it's a direct import
	Indirect bool   // Whether it's an indirect import (transitively required)
}

// PackageInfo represents metadata about a package
type PackageInfo struct {
	Path      string   // Full package path
	Dir       string   // Directory path
	Name      string   // Package name
	Imports   []string // Direct imports
	Files     int      // Number of Go files
	Functions int      // Number of top-level functions
	Types     int      // Number of type declarations
}

// DependencyPath represents a path in the dependency graph
type DependencyPath struct {
	From  string   // Starting package
	To    string   // Ending package
	Path  []string // List of packages in path (including from and to)
	Depth int      // Number of hops
}

// PackageGraph manages the dependency graph for Go packages
type PackageGraph struct {
	packages map[string]*PackageInfo
	edges    map[string][]*ImportEdge
	fset     *token.FileSet
}

// NewPackageGraph creates a new dependency graph for a directory
func NewPackageGraph(rootDir string) (*PackageGraph, error) {
	pg := &PackageGraph{
		packages: make(map[string]*PackageInfo),
		edges:    make(map[string][]*ImportEdge),
		fset:     token.NewFileSet(),
	}

	if err := pg.buildGraph(rootDir, ""); err != nil {
		return nil, fmt.Errorf("failed to build graph: %w", err)
	}

	return pg, nil
}

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

		importsSet := make(map[string]bool)
		var funcCount, typeCount int

		for _, file := range pkg.Files {
			// Count top-level declarations
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

			// Extract imports
			for _, imp := range file.Imports {
				impPath := strings.Trim(imp.Path.Value, "\"")
				if !importsSet[impPath] {
					importsSet[impPath] = true
					info.Imports = append(info.Imports, impPath)

					// Create import edge
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

		// Register package
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
