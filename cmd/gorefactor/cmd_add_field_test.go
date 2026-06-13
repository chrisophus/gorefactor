package main

import (
	"strings"
	"testing"
)

func TestAddFieldAtEnd(t *testing.T) {
	t.Chdir(t.TempDir())
	path := writeTempGo(t, ".", "f.go",
		"package x\n\ntype User struct {\n\tName string\n}\n")

	if err := addFieldCommand([]string{path, "User", "Email string `json:\"email\"`"}); err != nil {
		t.Fatalf("add-field: %v", err)
	}
	got := readFile(t, path)
	if !strings.Contains(got, "Email string `json:\"email\"`") {
		t.Fatalf("field missing:\n%s", got)
	}
	if strings.Index(got, "Name") > strings.Index(got, "Email") {
		t.Fatalf("field should come after Name:\n%s", got)
	}
}

func TestAddFieldAfterPosition(t *testing.T) {
	t.Chdir(t.TempDir())
	path := writeTempGo(t, ".", "f.go",
		"package x\n\ntype User struct {\n\tName string\n\tAge  int\n}\n")

	if err := addFieldCommand([]string{path, "User", "Email string", "--after", "Name"}); err != nil {
		t.Fatalf("add-field --after: %v", err)
	}
	got := readFile(t, path)
	ni, ei, ai := strings.Index(got, "Name"), strings.Index(got, "Email"), strings.Index(got, "Age")
	if !(ni < ei && ei < ai) {
		t.Fatalf("Email should sit between Name and Age:\n%s", got)
	}
}

func TestAddFieldUpdatesPositionalLiterals(t *testing.T) {
	t.Chdir(t.TempDir())
	path := writeTempGo(t, ".", "f.go",
		"package x\n\ntype Pair struct {\n\tA int\n\tB int\n}\n")
	other := writeTempGo(t, ".", "g.go",
		"package x\n\nfunc mk() Pair {\n\treturn Pair{1, 2}\n}\n")

	if err := addFieldCommand([]string{path, "Pair", "C int", "--update-literals"}); err != nil {
		t.Fatalf("add-field --update-literals: %v", err)
	}
	if !strings.Contains(readFile(t, path), "C int") {
		t.Fatal("field not added")
	}
	got := readFile(t, other)
	if !strings.Contains(got, "Pair{A: 1, B: 2}") {
		t.Fatalf("positional literal should be keyed:\n%s", got)
	}
}

func TestAddFieldWarnsButProceedsOnPositionalLiterals(t *testing.T) {
	t.Chdir(t.TempDir())
	path := writeTempGo(t, ".", "f.go",
		"package x\n\ntype Pair struct {\n\tA int\n\tB int\n}\n\nfunc mk() Pair {\n\treturn Pair{1, 2}\n}\n")

	if err := addFieldCommand([]string{path, "Pair", "C int"}); err != nil {
		t.Fatalf("add-field: %v", err)
	}
	got := readFile(t, path)
	if !strings.Contains(got, "C int") {
		t.Fatal("field should be added despite warning")
	}
	if !strings.Contains(got, "Pair{1, 2}") {
		t.Fatalf("without --update-literals the literal must stay positional:\n%s", got)
	}
}

func TestAddFieldRejectsBadSpec(t *testing.T) {
	t.Chdir(t.TempDir())
	path := writeTempGo(t, ".", "f.go", "package x\n\ntype T struct{}\n")
	before := readFile(t, path)

	for _, spec := range []string{"not a valid ( field", "A int\nB int"} {
		err := addFieldCommand([]string{path, "T", spec})
		assertExitCode(t, err, exitParseError)
	}
	if readFile(t, path) != before {
		t.Fatal("rejected spec must not modify the file")
	}
}

func TestAddFieldMissingStruct(t *testing.T) {
	t.Chdir(t.TempDir())
	path := writeTempGo(t, ".", "f.go", "package x\n\ntype Actual struct{}\n")

	err := addFieldCommand([]string{path, "Missing", "A int"})
	assertExitCode(t, err, exitNotFound)
	if !strings.Contains(err.Error(), "Actual") {
		t.Fatalf("error should list candidates: %v", err)
	}

	err = addFieldCommand([]string{path, "Actual", "A int", "--after", "Nope"})
	assertExitCode(t, err, exitNotFound)
}
