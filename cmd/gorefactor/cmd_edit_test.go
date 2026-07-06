package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEditShouldFallback(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"parse error → fallback", parseErrorf("bad snippet"), true},
		{"no-statement-match → fallback", notFoundErrorf("no statement matching %q found", "x"), true},
		{"plain not-found → no fallback", notFoundErrorf("function %q not found", "F"), false},
		{"usage error → no fallback", usageErrorf("bad args"), false},
		{"non-cliError → no fallback", os.ErrNotExist, false},
	}
	for _, c := range cases {
		if got := editShouldFallback(c.err); got != c.want {
			t.Errorf("%s: editShouldFallback = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestEditFallsBackToTextReplace(t *testing.T) {
	dir := t.TempDir()
	prev, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(prev) })

	// "1, 2" is not a complete statement → edit must fall back to replace-text.
	src := "package p\n\nfunc F() int {\n\treturn compute(1, 2)\n}\n\nfunc compute(a, b int) int { return a + b }\n"
	if err := os.WriteFile("m.go", []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := editCommand([]string{"m.go", "F", "1, 2", "3, 4"}); err != nil {
		t.Fatalf("edit fallback failed: %v", err)
	}
	out, _ := os.ReadFile(filepath.Join(dir, "m.go"))
	if want := "compute(3, 4)"; !strings.Contains(string(out), want) {
		t.Errorf("expected %q in result, got:\n%s", want, out)
	}
}

func TestEditStatementExactPath(t *testing.T) {
	dir := t.TempDir()
	prev, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(prev) })

	src := "package p\n\nfunc F() int {\n\tx := 1\n\treturn x\n}\n"
	if err := os.WriteFile("m.go", []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	// A complete statement → the statement-exact path applies it.
	if err := editCommand([]string{"m.go", "F", "x := 1", "x := 2"}); err != nil {
		t.Fatalf("edit statement path failed: %v", err)
	}
	out, _ := os.ReadFile(filepath.Join(dir, "m.go"))
	if !strings.Contains(string(out), "x := 2") {
		t.Errorf("expected statement replaced, got:\n%s", out)
	}
}
