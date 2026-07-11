package analyzer

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const nonAdjacentSrc = `package main

import (
	"log/slog"
)

func Audit(id string) error {
	if err := read(id); err != nil {
		slog.Error("audit failed", "err", err)
		id = ""
		_ = id
		return err
	}
	return nil
}

func read(id string) error { return nil }
`

// The safe pass must keep ignoring non-adjacent log/return pairs.
func TestApplyLogReturnFixes_NonAdjacentUntouchedWhenSafe(t *testing.T) {
	out, sites, err := ApplyLogReturnFixes("main.go", []byte(nonAdjacentSrc), "", false)
	if err != nil {
		t.Fatalf("ApplyLogReturnFixes: %v", err)
	}
	if out != nil || len(sites) != 0 {
		t.Fatalf("safe mode fixed a non-adjacent site: %d sites", len(sites))
	}
}

// The aggressive pass deletes the log and wraps the bare return even with
// statements between them.
func TestApplyLogReturnFixes_NonAdjacentFixedWhenAggressive(t *testing.T) {
	out, sites, err := ApplyLogReturnFixes("main.go", []byte(nonAdjacentSrc), "", true)
	if err != nil {
		t.Fatalf("ApplyLogReturnFixes: %v", err)
	}
	if len(sites) != 1 {
		t.Fatalf("sites = %d, want 1", len(sites))
	}
	got := string(out)
	if strings.Contains(got, "slog.Error") {
		t.Errorf("log statement not deleted:\n%s", got)
	}
	if !strings.Contains(got, `fmt.Errorf("read: %w", err)`) {
		t.Errorf("bare return not wrapped:\n%s", got)
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "main.go", out, 0); err != nil {
		t.Fatalf("aggressive fix produced unparsable Go: %v", err)
	}
}

// Two logs ahead of the same flagged return: both logs go, the return is
// wrapped exactly once (overlapping wrap edits would corrupt the file).
func TestApplyLogReturnFixes_TwoLogsOneReturn(t *testing.T) {
	const src = `package main

import (
	"log/slog"
)

func Audit(id string) error {
	if err := read(id); err != nil {
		slog.Error("first", "err", err)
		slog.Error("second", "err", err)
		return err
	}
	return nil
}

func read(id string) error { return nil }
`
	out, sites, err := ApplyLogReturnFixes("main.go", []byte(src), "", true)
	if err != nil {
		t.Fatalf("ApplyLogReturnFixes: %v", err)
	}
	if len(sites) != 2 {
		t.Fatalf("sites = %d, want 2 (both logs deleted)", len(sites))
	}
	got := string(out)
	if strings.Contains(got, "slog.Error") {
		t.Errorf("a log statement survived:\n%s", got)
	}
	if n := strings.Count(got, "fmt.Errorf"); n != 1 {
		t.Errorf("return wrapped %d times, want exactly once:\n%s", n, got)
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "main.go", out, 0); err != nil {
		t.Fatalf("fix produced unparsable Go: %v", err)
	}
}

// ListLogReturnFixSites mirrors the fixer: the non-adjacent site is listed
// only when aggressive is set.
func TestListLogReturnFixSites_AggressiveWidens(t *testing.T) {
	path := filepath.Join(t.TempDir(), "main.go")
	if err := os.WriteFile(path, []byte(nonAdjacentSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	safe, err := ListLogReturnFixSites(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(safe) != 0 {
		t.Fatalf("safe sites = %d, want 0", len(safe))
	}
	agg, err := ListLogReturnFixSites(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(agg) != 1 {
		t.Fatalf("aggressive sites = %d, want 1", len(agg))
	}
}
