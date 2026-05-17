package analyzer

import (
	"fmt"
	"go/token"
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
