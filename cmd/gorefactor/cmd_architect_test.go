package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chrisophus/gorefactor/analyzer"
)

func TestArchComponentName(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"auth", "auth"},
		{"auth/jwt", "auth_jwt"},
		{"api/v1/handler", "api_v1_handler"},
	}
	for _, tc := range tests {
		got := archComponentName(tc.path)
		if got != tc.want {
			t.Errorf("archComponentName(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

func TestArchComponentNameCollision(t *testing.T) {
	// Create a temporary directory with packages that would collide
	tmpdir := t.TempDir()

	// Create go.mod
	if err := os.WriteFile(filepath.Join(tmpdir, "go.mod"), []byte("module test\n"), 0o644); err != nil {
		t.Fatalf("Failed to create go.mod: %v", err)
	}

	// Create foo_bar package
	if err := os.Mkdir(filepath.Join(tmpdir, "foo_bar"), 0o755); err != nil {
		t.Fatalf("Failed to create foo_bar dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpdir, "foo_bar", "main.go"), []byte("package foo_bar\n"), 0o644); err != nil {
		t.Fatalf("Failed to create foo_bar file: %v", err)
	}

	// Create foo/bar package
	if err := os.MkdirAll(filepath.Join(tmpdir, "foo", "bar"), 0o755); err != nil {
		t.Fatalf("Failed to create foo/bar dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpdir, "foo", "bar", "main.go"), []byte("package bar\n"), 0o644); err != nil {
		t.Fatalf("Failed to create foo/bar file: %v", err)
	}

	// Load the graph
	graph, err := analyzer.NewPackageGraph(tmpdir)
	if err != nil {
		t.Fatalf("Failed to create package graph: %v", err)
	}

	yaml := generateArchYAML(graph)
	if !strings.Contains(yaml, "ERROR") || !strings.Contains(yaml, "collision") {
		t.Errorf("generateArchYAML should report collision for foo_bar and foo/bar\n%s", yaml)
	}
}

func TestArchLocalDepsLongestMatch(t *testing.T) {
	// Test that longest matching package path is preferred
	// When import matches both "bar" and "foo/bar", prefer "foo/bar"
	pkgs := []*analyzer.PackageInfo{
		{Path: "bar"},
		{Path: "foo/bar"},
	}
	pkg := &analyzer.PackageInfo{
		Path:    "myapp",
		Imports: []string{"github.com/foo/bar"},
	}

	deps := archLocalDeps(pkg, pkgs)
	if len(deps) != 1 || deps[0] != "foo_bar" {
		t.Errorf("archLocalDeps should prefer longest match (foo/bar), got %v", deps)
	}
}

func TestGenerateArchYAMLBasic(t *testing.T) {
	tmpdir := t.TempDir()

	// Create go.mod
	if err := os.WriteFile(filepath.Join(tmpdir, "go.mod"), []byte("module test\n"), 0o644); err != nil {
		t.Fatalf("Failed to create go.mod: %v", err)
	}

	// Create packages: api, auth, db, handlers
	for _, name := range []string{"api", "auth", "db", "handlers"} {
		dir := filepath.Join(tmpdir, name)
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatalf("Failed to create %s dir: %v", name, err)
		}
		content := "package " + name + "\n"
		if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(content), 0o644); err != nil {
			t.Fatalf("Failed to create %s file: %v", name, err)
		}
	}

	// Load the graph (it will discover the packages)
	graph, err := analyzer.NewPackageGraph(tmpdir)
	if err != nil {
		t.Fatalf("Failed to create package graph: %v", err)
	}

	yaml := generateArchYAML(graph)

	// Check for required sections
	if !strings.Contains(yaml, "version: 3") {
		t.Errorf("YAML should contain version 3")
	}
	if !strings.Contains(yaml, "components:") {
		t.Errorf("YAML should contain components section")
	}
	if !strings.Contains(yaml, "deps:") {
		t.Errorf("YAML should contain deps section")
	}
}

func TestGenerateArchYAMLEmptyDeps(t *testing.T) {
	tmpdir := t.TempDir()

	// Create go.mod
	if err := os.WriteFile(filepath.Join(tmpdir, "go.mod"), []byte("module test\n"), 0o644); err != nil {
		t.Fatalf("Failed to create go.mod: %v", err)
	}

	// Create a package with no imports
	if err := os.Mkdir(filepath.Join(tmpdir, "root"), 0o755); err != nil {
		t.Fatalf("Failed to create root dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpdir, "root", "main.go"), []byte("package root\n"), 0o644); err != nil {
		t.Fatalf("Failed to create root file: %v", err)
	}

	graph, err := analyzer.NewPackageGraph(tmpdir)
	if err != nil {
		t.Fatalf("Failed to create package graph: %v", err)
	}

	yaml := generateArchYAML(graph)

	if !strings.Contains(yaml, "mayDependOn: []") {
		t.Errorf("YAML should show empty mayDependOn for packages with no deps")
	}
}

func TestArchitectCommandBasic(t *testing.T) {
	// Create a temporary directory with a simple Go module
	tmpdir := t.TempDir()

	// Create go.mod
	modPath := filepath.Join(tmpdir, "go.mod")
	if err := os.WriteFile(modPath, []byte("module testmod\n\ngo 1.20\n"), 0o644); err != nil {
		t.Fatalf("Failed to create go.mod: %v", err)
	}

	// Create a simple package
	pkgDir := filepath.Join(tmpdir, "pkg")
	if err := os.Mkdir(pkgDir, 0o755); err != nil {
		t.Fatalf("Failed to create package dir: %v", err)
	}

	pkgFile := filepath.Join(pkgDir, "main.go")
	if err := os.WriteFile(pkgFile, []byte("package pkg\n\nfunc Hello() string { return \"hi\" }\n"), 0o644); err != nil {
		t.Fatalf("Failed to create Go file: %v", err)
	}

	// Run architect command with output to temp file
	outPath := filepath.Join(tmpdir, "output.yml")
	args := []string{"--suggest", "--output", outPath, tmpdir}
	if err := architectCommand(args); err != nil {
		t.Fatalf("architectCommand failed: %v", err)
	}

	// Verify output file was created
	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("Failed to read output file: %v", err)
	}

	yaml := string(content)
	if !strings.Contains(yaml, "version: 3") {
		t.Errorf("Generated YAML should contain version 3, got: %s", yaml)
	}
	if !strings.Contains(yaml, "components:") {
		t.Errorf("Generated YAML should contain components section")
	}
}
