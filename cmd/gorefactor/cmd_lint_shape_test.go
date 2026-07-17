package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFatalInLibraryRule(t *testing.T) {
	dir := t.TempDir()
	lib := filepath.Join(dir, "lib.go")
	src := `package lib

import (
	"log"
	"os"
)

func bad() {
	log.Fatal("boom")
	log.Fatalf("boom %d", 1)
	os.Exit(1)
}

func guard(n int) {
	if n < 0 {
		panic("unreachable")
	}
}
`
	if err := os.WriteFile(lib, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	issues := fatalInLibraryRule{}.Run(LintContext{Root: dir, Files: []string{lib}})
	warnings, infos := 0, 0
	for _, iss := range issues {
		switch iss.Severity {
		case "warning":
			warnings++
		case "info":
			infos++
		}
	}
	if warnings != 3 {
		t.Errorf("warnings = %d, want 3 (log.Fatal, log.Fatalf, os.Exit): %+v", warnings, issues)
	}
	if infos != 1 {
		t.Errorf("infos = %d, want 1 (panic): %+v", infos, issues)
	}
}

func TestFatalInLibraryRule_ExemptsMainAndTests(t *testing.T) {
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "main.go")
	mainSrc := "package main\n\nimport \"os\"\n\nfunc main() { os.Exit(1) }\n"
	if err := os.WriteFile(mainFile, []byte(mainSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	testFile := filepath.Join(dir, "lib_test.go")
	testSrc := "package lib\n\nfunc helper() { panic(\"in test file\") }\n"
	if err := os.WriteFile(testFile, []byte(testSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	issues := fatalInLibraryRule{}.Run(LintContext{Root: dir, Files: []string{mainFile, testFile}})
	if len(issues) != 0 {
		t.Fatalf("main packages and test files are exempt: %+v", issues)
	}
}

func TestFatalInLibraryRule_MessageNamesCall(t *testing.T) {
	dir := t.TempDir()
	lib := filepath.Join(dir, "lib.go")
	src := "package lib\n\nimport \"os\"\n\nfunc bad() { os.Exit(2) }\n"
	if err := os.WriteFile(lib, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	issues := fatalInLibraryRule{}.Run(LintContext{Root: dir, Files: []string{lib}})
	if len(issues) != 1 || !strings.Contains(issues[0].Message, "os.Exit") {
		t.Fatalf("issues = %+v, want one naming os.Exit", issues)
	}
}
