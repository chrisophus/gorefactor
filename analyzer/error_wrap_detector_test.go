package analyzer

import (
	"os"
	"path/filepath"
	"testing"
)

// TestFileErrorWrapIssuesSkipsFuncLitReturns is a regression test for the
// false-positive where `return err` inside a filepath.Walk/WalkDir callback
// (a *ast.FuncLit) was reported as an unwrapped error on the outer exported
// function. The fix is to stop ast.Inspect from descending into FuncLit nodes.
func TestFileErrorWrapIssuesSkipsFuncLitReturns(t *testing.T) {
	src := `package mypkg

import (
	"io/fs"
	"path/filepath"
)

// WalkFiles walks root and collects .go paths.
func WalkFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err  // ← inside FuncLit: must NOT be flagged
		}
		if d.IsDir() {
			return nil
		}
		files = append(files, path)
		return nil
	})
	return files, err
}
`
	dir := t.TempDir()
	f := filepath.Join(dir, "walk.go")
	if err := os.WriteFile(f, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	issues, err := FileErrorWrapIssues(f)
	if err != nil {
		t.Fatalf("FileErrorWrapIssues: %v", err)
	}
	// The `return err` at line 13 inside the FuncLit must not be reported.
	// The outer `return files, err` at line 21 may legitimately be reported
	// (it is a real unwrapped error on the outer function). Fail only if any
	// issue points at the line inside the callback.
	for _, iss := range issues {
		if iss.Line == 13 {
			t.Errorf("FuncLit return err was falsely reported at line %d: %s", iss.Line, iss.Message)
		}
	}
}

// TestFileErrorWrapIssuesFindsOuterBareReturn confirms that a bare `return err`
// directly in the exported function body (not inside a closure) is still flagged.
func TestFileErrorWrapIssuesFindsOuterBareReturn(t *testing.T) {
	src := `package mypkg

import "os"

func ReadFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return data, nil
}
`
	dir := t.TempDir()
	f := filepath.Join(dir, "read.go")
	if err := os.WriteFile(f, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	issues, err := FileErrorWrapIssues(f)
	if err != nil {
		t.Fatalf("FileErrorWrapIssues: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d: %v", len(issues), issues)
	}
}
