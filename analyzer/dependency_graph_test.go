package analyzer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewPackageGraph_BuildsGraph(t *testing.T) {
	t.Parallel()
	root := buildDepTestDir(t)

	pg, err := NewPackageGraph(root)
	if err != nil {
		t.Fatalf("NewPackageGraph: %v", err)
	}
	if pg == nil {
		t.Fatal("expected non-nil PackageGraph")
	}
}

func TestPackageGraph_AllPackages_CountsPackages(t *testing.T) {
	t.Parallel()
	root := buildDepTestDir(t)

	pg, err := NewPackageGraph(root)
	if err != nil {
		t.Fatal(err)
	}

	pkgs := pg.AllPackages()
	if len(pkgs) != 2 {
		t.Errorf("AllPackages() = %d packages, want 2", len(pkgs))
	}
}

func TestPackageGraph_GetPackage_RootPackage(t *testing.T) {
	t.Parallel()
	root := buildDepTestDir(t)

	pg, err := NewPackageGraph(root)
	if err != nil {
		t.Fatal(err)
	}

	// Root directory gets pkgPath ""
	info := pg.GetPackage("")
	if info == nil {
		t.Fatal("GetPackage(\"\") returned nil")
	}
	if info.Name != "main" {
		t.Errorf("root package name = %q, want %q", info.Name, "main")
	}
	if info.Files != 1 {
		t.Errorf("root package Files = %d, want 1", info.Files)
	}
}

func TestPackageGraph_GetPackage_SubPackage(t *testing.T) {
	t.Parallel()
	root := buildDepTestDir(t)

	pg, err := NewPackageGraph(root)
	if err != nil {
		t.Fatal(err)
	}

	info := pg.GetPackage("sub")
	if info == nil {
		t.Fatal("GetPackage(\"sub\") returned nil")
	}
	if info.Name != "sub" {
		t.Errorf("sub package name = %q, want %q", info.Name, "sub")
	}
}

func TestPackageGraph_GetPackage_NotFound(t *testing.T) {
	t.Parallel()
	root := buildDepTestDir(t)

	pg, err := NewPackageGraph(root)
	if err != nil {
		t.Fatal(err)
	}

	if info := pg.GetPackage("nonexistent"); info != nil {
		t.Errorf("expected nil for nonexistent package, got %+v", info)
	}
}

func TestPackageGraph_GetDependencies_ReturnsEdges(t *testing.T) {
	t.Parallel()
	root := buildDepTestDir(t)

	pg, err := NewPackageGraph(root)
	if err != nil {
		t.Fatal(err)
	}

	// Root package imports "sub"
	edges := pg.GetDependencies("")
	if len(edges) == 0 {
		t.Fatal("expected at least one edge from root package")
	}
	found := false
	for _, e := range edges {
		if e.To == "sub" && e.Direct {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected edge To=sub, got edges: %+v", edges)
	}
}

func TestPackageGraph_GetDependencies_UnknownPackage(t *testing.T) {
	t.Parallel()
	root := buildDepTestDir(t)

	pg, err := NewPackageGraph(root)
	if err != nil {
		t.Fatal(err)
	}

	edges := pg.GetDependencies("nonexistent")
	if edges != nil {
		t.Errorf("expected nil edges for unknown package, got %v", edges)
	}
}

func TestPackageGraph_FindPath_DirectEdge(t *testing.T) {
	t.Parallel()
	root := buildDepTestDir(t)

	pg, err := NewPackageGraph(root)
	if err != nil {
		t.Fatal(err)
	}

	// Root imports "sub", so path from "" to "sub" exists via the import edge.
	path := pg.FindPath("", "sub")
	if path == nil {
		t.Fatal("FindPath returned nil for existing direct path")
	}
	if path.From != "" || path.To != "sub" {
		t.Errorf("path From/To = %q/%q, want empty/sub", path.From, path.To)
	}
	if len(path.Path) < 2 {
		t.Errorf("expected at least 2 nodes in path, got %v", path.Path)
	}
}

func TestPackageGraph_FindPath_NoPath(t *testing.T) {
	t.Parallel()
	root := buildDepTestDir(t)

	pg, err := NewPackageGraph(root)
	if err != nil {
		t.Fatal(err)
	}

	// sub does not import "" (the root), so there is no path from "sub" to "".
	path := pg.FindPath("sub", "")
	if path != nil {
		t.Errorf("expected nil path, got %+v", path)
	}
}

func TestPackageGraph_HasCircularDependencies_NoCycle(t *testing.T) {
	t.Parallel()
	root := buildDepTestDir(t)

	pg, err := NewPackageGraph(root)
	if err != nil {
		t.Fatal(err)
	}

	cycles := pg.HasCircularDependencies()
	if len(cycles) != 0 {
		t.Errorf("expected no cycles, got %v", cycles)
	}
}

func TestPackageGraph_Summary_ContainsPackageCount(t *testing.T) {
	t.Parallel()
	root := buildDepTestDir(t)

	pg, err := NewPackageGraph(root)
	if err != nil {
		t.Fatal(err)
	}

	summary := pg.Summary()
	if summary == "" {
		t.Fatal("Summary() returned empty string")
	}
	if !strings.Contains(summary, "2") {
		t.Errorf("expected summary to mention 2 packages, got: %s", summary)
	}
	if !strings.Contains(summary, "No circular") {
		t.Errorf("expected 'No circular' in summary, got: %s", summary)
	}
}

func TestPackageGraph_PrintGraph_NonEmpty(t *testing.T) {
	t.Parallel()
	root := buildDepTestDir(t)

	pg, err := NewPackageGraph(root)
	if err != nil {
		t.Fatal(err)
	}

	out := pg.PrintGraph()
	if out == "" {
		t.Fatal("PrintGraph() returned empty string")
	}
}

func TestPackageGraph_EmptyDirectory(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	pg, err := NewPackageGraph(root)
	if err != nil {
		t.Fatalf("NewPackageGraph on empty dir: %v", err)
	}
	if len(pg.AllPackages()) != 0 {
		t.Errorf("expected 0 packages in empty dir, got %d", len(pg.AllPackages()))
	}
	if cycles := pg.HasCircularDependencies(); len(cycles) != 0 {
		t.Errorf("expected no cycles in empty graph, got %v", cycles)
	}
}

func TestPackageGraph_SkipsVendorAndHiddenDirs(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	// Create vendor and hidden directories that should be skipped.
	for _, d := range []string{"vendor", ".git"} {
		dir := filepath.Join(root, d)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		src := `package hidden
`
		if err := os.WriteFile(filepath.Join(dir, "h.go"), []byte(src), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Add a real package.
	if err := os.WriteFile(filepath.Join(root, "real.go"), []byte("package real\n"), 0644); err != nil {
		t.Fatal(err)
	}

	pg, err := NewPackageGraph(root)
	if err != nil {
		t.Fatal(err)
	}

	for _, pkg := range pg.AllPackages() {
		if pkg.Name == "hidden" {
			t.Errorf("vendor/.git package should be excluded, but found: %+v", pkg)
		}
	}
}

// buildDepTestDir creates a minimal two-package directory structure:
//
//	rootDir/
//	  main.go   (package main, imports "sub")
//	  sub/
//	    helper.go  (package sub, imports "fmt")
//
// Using the bare import path "sub" (not a real module path) is sufficient
// because PackageGraph only string-matches import paths, never loads them.
func buildDepTestDir(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	mainSrc := `package main

import "sub"

func main() { _ = sub.Help() }
`
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte(mainSrc), 0644); err != nil {
		t.Fatal(err)
	}

	subDir := filepath.Join(root, "sub")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	helperSrc := `package sub

import "fmt"

func Help() string { return fmt.Sprintf("help") }
`
	if err := os.WriteFile(filepath.Join(subDir, "helper.go"), []byte(helperSrc), 0644); err != nil {
		t.Fatal(err)
	}
	return root
}
