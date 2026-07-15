package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeGoFile(t *testing.T, dir, name, src string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return p
}

func TestExtractVar_Simple(t *testing.T) {
	dir := t.TempDir()
	f := writeGoFile(t, dir, "a.go", "package p\n\nfunc F(a, b int) int {\n\tx := a + b\n\treturn x\n}\n")
	if err := runExtractVar([]string{f, "F", "a + b", "sum"}, false); err != nil {
		t.Fatalf("extract-var: %v", err)
	}
	got, _ := os.ReadFile(f)
	s := string(got)
	if !strings.Contains(s, "sum := a + b") || !strings.Contains(s, "x := sum") {
		t.Errorf("unexpected output:\n%s", s)
	}
}

func TestExtractVar_NestedAnchorStaysInBlock(t *testing.T) {
	dir := t.TempDir()
	src := "package p\n\nfunc F(a, b int) int {\n\ttotal := 0\n\tif a > 0 {\n\t\ttotal = a + b + 1\n\t}\n\treturn total\n}\n"
	f := writeGoFile(t, dir, "a.go", src)
	if err := runExtractVar([]string{f, "F", "a + b + 1", "tmp"}, false); err != nil {
		t.Fatalf("extract-var: %v", err)
	}
	got, _ := os.ReadFile(f)
	s := string(got)
	// The binding must be inside the if block (indented two tabs), not before it.
	if !strings.Contains(s, "\t\ttmp := a + b + 1") {
		t.Errorf("binding not placed inside the if block:\n%s", s)
	}
	if !strings.Contains(s, "total = tmp") {
		t.Errorf("occurrence not rewritten:\n%s", s)
	}
}

func TestExtractVar_InConditionAnchorsBeforeIf(t *testing.T) {
	dir := t.TempDir()
	src := "package p\n\nfunc F(a, b int) int {\n\tif a+b > 10 {\n\t\treturn 1\n\t}\n\treturn 0\n}\n"
	f := writeGoFile(t, dir, "a.go", src)
	if err := runExtractVar([]string{f, "F", "a+b", "s"}, false); err != nil {
		t.Fatalf("extract-var: %v", err)
	}
	s := string(mustRead(t, f))
	// Binding one tab in, immediately before the if; condition rewritten to s.
	if !strings.Contains(s, "\ts := a + b\n\tif s > 10") {
		t.Errorf("binding not anchored before the if:\n%s", s)
	}
}

func TestExtractVar_AllReplacesEveryOccurrence(t *testing.T) {
	dir := t.TempDir()
	src := "package p\n\nfunc F(a, b int) int {\n\tp := a * b\n\tq := a * b\n\treturn p + q\n}\n"
	f := writeGoFile(t, dir, "a.go", src)
	if err := runExtractVar([]string{f, "F", "a * b", "prod", "--all"}, false); err != nil {
		t.Fatalf("extract-var --all: %v", err)
	}
	s := string(mustRead(t, f))
	if strings.Count(s, "prod") < 3 { // decl + two uses
		t.Errorf("--all did not rewrite both occurrences:\n%s", s)
	}
	if strings.Contains(s, "a * b\n\tq := a * b") {
		t.Errorf("second occurrence not rewritten:\n%s", s)
	}
}

func TestExtractConst_RejectsLocalVarExpr(t *testing.T) {
	dir := t.TempDir()
	f := writeGoFile(t, dir, "a.go", "package p\n\nfunc F(a, b int) int {\n\treturn a + b\n}\n")
	err := runExtractVar([]string{f, "F", "a + b", "sum"}, true)
	if err == nil || !strings.Contains(err.Error(), "cannot be a constant") {
		t.Fatalf("expected const rejection for param-referencing expr, got %v", err)
	}
}

func TestExtractConst_RejectsCall(t *testing.T) {
	dir := t.TempDir()
	f := writeGoFile(t, dir, "a.go", "package p\n\nimport \"strings\"\n\nfunc F() int {\n\treturn len(strings.Repeat(\"x\", 3))\n}\n")
	err := runExtractVar([]string{f, "F", "strings.Repeat(\"x\", 3)", "r"}, true)
	if err == nil || !strings.Contains(err.Error(), "function call") {
		t.Fatalf("expected rejection for call in const, got %v", err)
	}
}

func TestExtractConst_AcceptsLiteral(t *testing.T) {
	dir := t.TempDir()
	f := writeGoFile(t, dir, "a.go", "package p\n\nfunc F() int {\n\treturn 60 * 60\n}\n")
	if err := runExtractVar([]string{f, "F", "60 * 60", "sph"}, true); err != nil {
		t.Fatalf("extract-const literal: %v", err)
	}
	s := string(mustRead(t, f))
	if !strings.Contains(s, "const sph = 60 * 60") {
		t.Errorf("const not emitted:\n%s", s)
	}
}

func TestExtractVar_NotFound(t *testing.T) {
	dir := t.TempDir()
	f := writeGoFile(t, dir, "a.go", "package p\n\nfunc F(a int) int { return a }\n")
	if err := runExtractVar([]string{f, "F", "z + z", "v"}, false); err == nil {
		t.Fatal("expected not-found error")
	}
}

func TestExtractVar_InvalidIdent(t *testing.T) {
	dir := t.TempDir()
	f := writeGoFile(t, dir, "a.go", "package p\n\nfunc F(a, b int) int { return a + b }\n")
	if err := runExtractVar([]string{f, "F", "a + b", "2bad"}, false); err == nil {
		t.Fatal("expected invalid-identifier error")
	}
}

func TestIsValidIdent(t *testing.T) {
	for _, ok := range []string{"x", "sum", "_x", "camelCase", "n1"} {
		if !isValidIdent(ok) {
			t.Errorf("%q should be valid", ok)
		}
	}
	for _, bad := range []string{"", "_", "2x", "a-b", "func", "return", "a.b"} {
		if isValidIdent(bad) {
			t.Errorf("%q should be invalid", bad)
		}
	}
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return b
}
