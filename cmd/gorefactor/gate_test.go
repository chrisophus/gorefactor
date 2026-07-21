package main

import (
	"os"
	"path/filepath"
	"testing"
)

// A build gate run inside a main-package directory must not leave the
// compiled executable behind (go build writes single main packages to the
// working directory unless redirected).
func TestGoGateBuildLeavesNoBinary(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module gatebin\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := goGate(dir, "build", "./..."); err != nil {
		t.Fatalf("goGate build: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "go.mod" && e.Name() != "main.go" {
			t.Errorf("gate left %q behind in the package dir", e.Name())
		}
	}
}
