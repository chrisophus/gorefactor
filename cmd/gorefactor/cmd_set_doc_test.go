package main

import (
	"strings"
	"testing"
)

func TestSetDocAddsCommentWithNamePrefix(t *testing.T) {
	t.Chdir(t.TempDir())
	path := writeTempGo(t, ".", "f.go", "package x\n\nfunc Run() {}\n")

	if err := setDocCommand([]string{path, "Run", "executes the main loop"}); err != nil {
		t.Fatalf("set-doc: %v", err)
	}
	got := readFile(t, path)
	if !strings.Contains(got, "// Run executes the main loop\nfunc Run() {}") {
		t.Fatalf("doc comment missing or misplaced:\n%s", got)
	}
}

func TestSetDocReplacesExistingDoc(t *testing.T) {
	t.Chdir(t.TempDir())
	path := writeTempGo(t, ".", "f.go",
		"package x\n\n// Run is stale documentation.\n// It spans two lines.\nfunc Run() {}\n")

	if err := setDocCommand([]string{path, "Run", "Run is fresh documentation."}); err != nil {
		t.Fatalf("set-doc: %v", err)
	}
	got := readFile(t, path)
	if strings.Contains(got, "stale") || strings.Contains(got, "two lines") {
		t.Fatalf("old doc should be fully replaced:\n%s", got)
	}
	if !strings.Contains(got, "// Run is fresh documentation.\nfunc Run() {}") {
		t.Fatalf("new doc missing:\n%s", got)
	}
}

func TestSetDocWrapsLongText(t *testing.T) {
	t.Chdir(t.TempDir())
	path := writeTempGo(t, ".", "f.go", "package x\n\ntype Thing struct{}\n")

	long := strings.Repeat("word ", 60)
	if err := setDocCommand([]string{path, "Thing", long}); err != nil {
		t.Fatalf("set-doc: %v", err)
	}
	for _, line := range strings.Split(readFile(t, path), "\n") {
		if strings.HasPrefix(line, "//") && len(line) > docCommentWidth {
			t.Fatalf("comment line exceeds %d cols: %q", docCommentWidth, line)
		}
	}
	if !strings.Contains(readFile(t, path), "// Thing word") {
		t.Fatal("doc should start with the declaration name")
	}
}

func TestSetDocMethodAndVarGroup(t *testing.T) {
	t.Chdir(t.TempDir())
	path := writeTempGo(t, ".", "f.go",
		"package x\n\ntype S struct{}\n\nfunc (s S) Do() {}\n\nvar (\n\tlimit = 1\n\tcount = 2\n)\n")

	if err := setDocCommand([]string{path, "S:Do", "Do performs the thing."}); err != nil {
		t.Fatalf("set-doc method: %v", err)
	}
	if !strings.Contains(readFile(t, path), "// Do performs the thing.\nfunc (s S) Do() {}") {
		t.Fatalf("method doc missing:\n%s", readFile(t, path))
	}

	if err := setDocCommand([]string{path, "limit", "limit bounds the batch size."}); err != nil {
		t.Fatalf("set-doc var group: %v", err)
	}
	if !strings.Contains(readFile(t, path), "// limit bounds the batch size.\nvar (") {
		t.Fatalf("var-group doc missing:\n%s", readFile(t, path))
	}
}

func TestSetDocMissingDecl(t *testing.T) {
	t.Chdir(t.TempDir())
	path := writeTempGo(t, ".", "f.go", "package x\n\nfunc Here() {}\n")
	before := readFile(t, path)

	err := setDocCommand([]string{path, "Gone", "text"})
	assertExitCode(t, err, exitNotFound)
	if !strings.Contains(err.Error(), "Here") {
		t.Fatalf("error should list candidates: %v", err)
	}
	if readFile(t, path) != before {
		t.Fatal("failed set-doc must not modify the file")
	}
}

func TestFormatDocComment(t *testing.T) {
	got := formatDocComment("Foo", "does things")
	if got != "// Foo does things\n" {
		t.Fatalf("formatDocComment = %q", got)
	}
	// Name already present: not duplicated.
	got = formatDocComment("Foo", "Foo does things")
	if got != "// Foo does things\n" {
		t.Fatalf("formatDocComment with name = %q", got)
	}
}
