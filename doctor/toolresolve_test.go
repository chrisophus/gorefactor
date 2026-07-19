package doctor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestToolBootstrapDisabled(t *testing.T) {
	t.Setenv(noToolBootstrapEnv, "1")
	if !ToolBootstrapDisabled() {
		t.Fatal("expected bootstrap disabled")
	}
	t.Setenv(noToolBootstrapEnv, "true")
	if !ToolBootstrapDisabled() {
		t.Fatal("expected true to disable bootstrap")
	}
	t.Setenv(noToolBootstrapEnv, "")
	if ToolBootstrapDisabled() {
		t.Fatal("expected bootstrap enabled by default")
	}
}

func TestFindGolangciLint_Cached(t *testing.T) {
	root := t.TempDir()
	dest := cachedGolangciLintPath(root)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := FindGolangciLint(root); got != dest {
		t.Fatalf("FindGolangciLint() = %q, want %q", got, dest)
	}
}

func TestEnsureGolangciLint_BootstrapDisabled(t *testing.T) {
	root := t.TempDir()
	t.Setenv(noToolBootstrapEnv, "1")
	if _, err := EnsureGolangciLint(root); err == nil {
		t.Fatal("expected error when bootstrap disabled and binary missing")
	}
}

func TestProbeModuleOrPathTool_GoTool(t *testing.T) {
	if err := probeModuleOrPathTool(".", "deadcode-not-installed-xyz", "deadcode"); err != nil {
		t.Fatalf("expected go tool deadcode to be available: %v", err)
	}
}
