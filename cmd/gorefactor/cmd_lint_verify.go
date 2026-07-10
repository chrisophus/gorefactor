package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// dirSnapshot captures the .go files of a single package directory so an autofix confined to that
// package can be reverted byte-for-byte when the verify gate rejects it. It records the content of
// every .go file that existed at snapshot time; restore rewrites those and deletes any .go file the
// fix newly created.
//
// A directory-scoped snapshot is sufficient because every fixable rule (file-size → split,
// dead-code → delete, error-not-wrapped → wrap-errors, the log-propagation rules →
// remove-log-return / wrap-sentinels) only touches the issue file's own package directory: split
// writes sibling files in the same dir, the others edit a single file in place. The gate itself is
// still project-wide, so a fix that compiles locally but breaks a downstream package is caught and
// reverted at its source directory.
type dirSnapshot struct {
	dir   string
	files map[string][]byte
}

// snapshotGoDir reads the current bytes of every .go file directly in dir
// (non-recursive — that matches Go's one-directory-per-package rule).
func snapshotGoDir(dir string) (dirSnapshot, error) {
	snap := dirSnapshot{dir: dir, files: map[string][]byte{}}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return snap, fmt.Errorf("read dir %s: %w", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return snap, fmt.Errorf("read %s: %w", p, rerr)
		}
		snap.files[p] = b
	}
	return snap, nil
}

// restore reverts the package directory to its snapshot: .go files created
// since the snapshot are removed and every captured file is rewritten to its
// original bytes (which also recreates any file the fix deleted).
func (s dirSnapshot) restore() error {
	if entries, err := os.ReadDir(s.dir); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
				continue
			}
			p := filepath.Join(s.dir, e.Name())
			if _, ok := s.files[p]; !ok {
				if rerr := os.Remove(p); rerr != nil {
					return fmt.Errorf("remove created file %s: %w", p, rerr)
				}
			}
		}
	}
	for p, b := range s.files {
		if werr := os.WriteFile(p, b, 0644); werr != nil {
			return fmt.Errorf("restore %s: %w", p, werr)
		}
	}
	return nil
}

// verifyGateFn is the pass/fail check applied after each autofix under
// --verify. It is a package variable so tests can substitute a cheap fake for
// the real toolchain gate.
var verifyGateFn = verifyGate

// verifyGate is doctor's gate minus the lint stage (lint is what produced the
// fix): the project must still build and its tests must still pass. `go build
// ./...` does not compile _test.go files, so the test stage is what catches a
// dead-code deletion that removes a symbol used only from tests.
func verifyGate(root string) error {
	if root == "" {
		root = "."
	}
	stages := []struct {
		label string
		args  []string
	}{
		{"go build ./...", []string{"build", "./..."}},
		{"go test ./...", []string{"test", "./..."}},
	}
	for _, st := range stages {
		cmd := exec.Command("go", st.args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("%s:\n%s", st.label, strings.TrimSpace(string(out)))
		}
	}
	return nil
}
