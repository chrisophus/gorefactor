package main

import (
	goparser "go/parser"
	"go/token"
	"strings"
	"testing"
)

func TestInsertSwitchCase(t *testing.T) {
	t.Chdir(t.TempDir())
	const src = `package x

func dispatch(name string) int {
	switch name {
	case "a":
		return 1
	default:
		return 0
	}
}
`
	path := writeTempGo(t, ".", "d.go", src)
	captureStdout(t, func() {
		if err := insertSwitchCaseCommand([]string{path, "dispatch", `"b"`, "return 2"}); err != nil {
			t.Fatalf("insert-switch-case: %v", err)
		}
	})
	got := readFile(t, path)
	mustParse(t, path)
	// New case is present and lands BEFORE default.
	if !strings.Contains(got, `case "b":`) || !strings.Contains(got, "return 2") {
		t.Fatalf("new case missing:\n%s", got)
	}
	if strings.Index(got, `case "b":`) > strings.Index(got, "default:") {
		t.Fatalf("new case should precede default:\n%s", got)
	}
}

func TestInsertSwitchCaseNoSwitch(t *testing.T) {
	t.Chdir(t.TempDir())
	path := writeTempGo(t, ".", "n.go", "package x\n\nfunc f() int { return 1 }\n")
	err := insertSwitchCaseCommand([]string{path, "f", `"b"`, "return 2"})
	assertExitCode(t, err, exitNotFound)
}

func TestInsertMapEntryVar(t *testing.T) {
	t.Chdir(t.TempDir())
	const src = `package x

var tools = map[string]bool{
	"a": true,
	"b": true,
}
`
	path := writeTempGo(t, ".", "m.go", src)
	captureStdout(t, func() {
		if err := insertMapEntryCommand([]string{path, "tools", `"c": true`}); err != nil {
			t.Fatalf("insert-map-entry (var): %v", err)
		}
	})
	got := readFile(t, path)
	mustParse(t, path)
	if !strings.Contains(got, `"c": true`) {
		t.Fatalf("map entry missing:\n%s", got)
	}
}

func TestInsertMapEntryFuncSlice(t *testing.T) {
	t.Chdir(t.TempDir())
	const src = `package x

func catalog() []string {
	return []string{
		"x",
		"y",
	}
}
`
	path := writeTempGo(t, ".", "c.go", src)
	captureStdout(t, func() {
		if err := insertMapEntryCommand([]string{path, "catalog", `"z"`}); err != nil {
			t.Fatalf("insert-map-entry (func slice): %v", err)
		}
	})
	got := readFile(t, path)
	mustParse(t, path)
	if !strings.Contains(got, `"z"`) {
		t.Fatalf("slice element missing:\n%s", got)
	}
}

// TestInsertMapEntryTrailingComma: an element written with a trailing comma
// (how a model naturally writes a map entry) must not produce a double comma.
func TestInsertMapEntryTrailingComma(t *testing.T) {
	t.Chdir(t.TempDir())
	path := writeTempGo(t, ".", "tc.go", "package x\n\nvar m = map[string]bool{\n\t\"a\": true,\n}\n")
	captureStdout(t, func() {
		if err := insertMapEntryCommand([]string{path, "m", `"b": true,`}); err != nil {
			t.Fatalf("insert-map-entry with trailing comma: %v", err)
		}
	})
	mustParse(t, path)
	if strings.Count(readFile(t, path), ",,") != 0 {
		t.Fatalf("trailing comma produced a double comma:\n%s", readFile(t, path))
	}
}

func TestInsertMapEntryNotFound(t *testing.T) {
	t.Chdir(t.TempDir())
	path := writeTempGo(t, ".", "e.go", "package x\n\nvar n = 1\n")
	err := insertMapEntryCommand([]string{path, "nope", `"z"`})
	assertExitCode(t, err, exitNotFound)
}

func TestReplaceInLiteral(t *testing.T) {
	t.Chdir(t.TempDir())
	const src = "package x\n\nfunc prompt() string {\n\treturn `line one\nline two`\n}\n"
	path := writeTempGo(t, ".", "p.go", src)
	captureStdout(t, func() {
		if err := replaceInLiteralCommand([]string{path, "line two", "line two\nline three"}); err != nil {
			t.Fatalf("replace-in-literal: %v", err)
		}
	})
	got := readFile(t, path)
	mustParse(t, path)
	if !strings.Contains(got, "line three") {
		t.Fatalf("replacement missing:\n%s", got)
	}
}

func TestReplaceInLiteralAmbiguous(t *testing.T) {
	t.Chdir(t.TempDir())
	const src = "package x\n\nvar a = \"dup\"\nvar b = \"dup\"\n"
	path := writeTempGo(t, ".", "a.go", src)
	err := replaceInLiteralCommand([]string{path, "dup", "changed"})
	assertExitCode(t, err, exitUsage)
}

func TestReplaceInLiteralNotFound(t *testing.T) {
	t.Chdir(t.TempDir())
	path := writeTempGo(t, ".", "nf.go", "package x\n\nvar a = \"hello\"\n")
	err := replaceInLiteralCommand([]string{path, "absent", "x"})
	assertExitCode(t, err, exitNotFound)
}

// TestEndOfFlagsSeparator verifies the POSIX "--" separator makes dash-leading
// values positional instead of parse errors — needed for replace-in-literal on
// markdown-list text like "- item".
func TestEndOfFlagsSeparator(t *testing.T) {
	spec := mutFlagSpec(nil)
	pos, _ := parseFlags([]string{"--", "-x", "- a dash item"}, spec)
	if len(pos) != 2 || pos[0] != "-x" || pos[1] != "- a dash item" {
		t.Fatalf("-- should make following args positional, got %v", pos)
	}
	// checkCommandArgs must also count them as positional (no unknown-flag error).
	cmd := Command{Name: "replace-in-literal", MinArgs: 3, MaxArgs: 3, Flags: spec}
	if err := checkCommandArgs(cmd, []string{"f.go", "--", "-old", "-new"}); err != nil {
		t.Fatalf("checkCommandArgs with -- should accept dash-leading positionals: %v", err)
	}
}

// mustParse fails the test if src at path is not valid Go.
func mustParse(t *testing.T, path string) {
	t.Helper()
	if _, err := goparser.ParseFile(token.NewFileSet(), path, nil, 0); err != nil {
		t.Fatalf("result does not parse: %v", err)
	}
}
