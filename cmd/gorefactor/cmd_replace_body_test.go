package main

import (
	"strings"
	"testing"

	"github.com/chrisophus/gorefactor/orchestrator"
)

func TestReplaceBodyStatementList(t *testing.T) {
	t.Chdir(t.TempDir())
	path := writeTempGo(t, ".", "f.go",
		"package x\n\nfunc Greet() string {\n\treturn \"hi\"\n}\n")

	if err := replaceBodyCommand([]string{path, "Greet", "msg := \"hello\"\nreturn msg"}); err != nil {
		t.Fatalf("replace-body: %v", err)
	}
	got := readFile(t, path)
	if !strings.Contains(got, "msg := \"hello\"") || !strings.Contains(got, "return msg") {
		t.Fatalf("body not replaced:\n%s", got)
	}
	if strings.Contains(got, "return \"hi\"") {
		t.Fatalf("old body should be gone:\n%s", got)
	}
	// The whole change is journaled and undoable.
	if err := undoRefactoring(nil); err != nil {
		t.Fatalf("undo: %v", err)
	}
	if !strings.Contains(readFile(t, path), "return \"hi\"") {
		t.Fatal("undo should restore the original body")
	}
}

func TestReplaceBodyBracedBlockAndMethod(t *testing.T) {
	t.Chdir(t.TempDir())
	path := writeTempGo(t, ".", "f.go",
		"package x\n\ntype S struct{ n int }\n\nfunc (s *S) Val() int {\n\treturn s.n\n}\n")

	if err := replaceBodyCommand([]string{path, "S:Val", "{\n\treturn s.n * 2\n}"}); err != nil {
		t.Fatalf("replace-body method: %v", err)
	}
	if !strings.Contains(readFile(t, path), "return s.n * 2") {
		t.Fatalf("method body not replaced:\n%s", readFile(t, path))
	}
}

func TestReplaceBodyMissingTarget(t *testing.T) {
	t.Chdir(t.TempDir())
	path := writeTempGo(t, ".", "f.go", "package x\n\nfunc Real() {}\n")
	before := readFile(t, path)

	err := replaceBodyCommand([]string{path, "Fake", "return"})
	assertExitCode(t, err, exitNotFound)
	if !strings.Contains(err.Error(), "Real") {
		t.Fatalf("error should list candidates: %v", err)
	}
	if readFile(t, path) != before {
		t.Fatal("failed replace-body must not modify the file")
	}
}

func TestReplaceBodyRejectsBadContent(t *testing.T) {
	t.Chdir(t.TempDir())
	path := writeTempGo(t, ".", "f.go", "package x\n\nfunc F() {}\n")
	before := readFile(t, path)

	err := replaceBodyCommand([]string{path, "F", "if broken {"})
	assertExitCode(t, err, exitParseError)
	if readFile(t, path) != before {
		t.Fatal("rejected content must not modify the file")
	}
	if entries, _ := orchestrator.LoadJournal(); len(entries) != 0 {
		t.Fatal("rejected replace-body must not be journaled")
	}
}

func TestReplaceBodyDryRun(t *testing.T) {
	t.Chdir(t.TempDir())
	path := writeTempGo(t, ".", "f.go", "package x\n\nfunc F() int {\n\treturn 1\n}\n")
	before := readFile(t, path)

	out := captureStdout(t, func() {
		if err := replaceBodyCommand([]string{path, "F", "return 2", "--dry-run"}); err != nil {
			t.Errorf("dry-run: %v", err)
		}
	})
	if readFile(t, path) != before {
		t.Fatal("--dry-run must not modify the file")
	}
	if !strings.Contains(out, "-\treturn 1") || !strings.Contains(out, "+\treturn 2") {
		t.Fatalf("dry-run should print a diff, got:\n%s", out)
	}
}

func TestNormalizeBodyBlock(t *testing.T) {
	cases := []struct {
		in   string
		ok   bool
		want string
	}{
		{"return 1", true, "{\nreturn 1\n}"},
		{"{ return 1 }", true, "{ return 1 }"},
		{"", true, "{\n}"},
		{"x := 1\ny := 2\n_ = x + y", true, "{\nx := 1\ny := 2\n_ = x + y\n}"},
		{"if {", false, ""},
	}
	for _, c := range cases {
		got, err := normalizeBodyBlock(c.in)
		if c.ok && err != nil {
			t.Errorf("normalizeBodyBlock(%q) unexpected error: %v", c.in, err)
			continue
		}
		if !c.ok {
			if err == nil {
				t.Errorf("normalizeBodyBlock(%q) should fail", c.in)
			} else {
				assertExitCode(t, err, exitParseError)
			}
			continue
		}
		if got != c.want {
			t.Errorf("normalizeBodyBlock(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
