package main

import (
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const addTestFuncSrc = `package greet

func Greet(name string, loud bool) string {
	if loud {
		return "HELLO " + name
	}
	return "hello " + name
}

func Add(a, b int) int {
	return a + b
}

func Noop() {
}
`

const addTestWithErrorSrc = `package greet

import "errors"

func Lookup(key string) (string, error) {
	if key == "" {
		return "", errors.New("empty key")
	}
	return key, nil
}
`

// addTestWriteModule sets up a temp module with greet package files.
func addTestWriteModule(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module greetmod\n\ngo 1.24\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestAddTestCreatesTestFile(t *testing.T) {
	addTestWriteModule(t, map[string]string{"greet.go": addTestFuncSrc})
	if err := addTestCommand([]string{"greet.go", "Greet"}); err != nil {
		t.Fatalf("add-test: %v", err)
	}
	if _, err := os.Stat("greet_test.go"); err != nil {
		t.Fatalf("greet_test.go should be created: %v", err)
	}
	src := readFile(t, "greet_test.go")
	if !strings.Contains(src, "func TestGreet(t *testing.T)") {
		t.Fatalf("TestGreet function not found in generated file:\n%s", src)
	}
	if !strings.Contains(src, "cases") {
		t.Fatalf("table-driven cases struct not found:\n%s", src)
	}
	if !strings.Contains(src, "t.Run(") {
		t.Fatalf("t.Run loop not found:\n%s", src)
	}
	// Ensure it parses.
	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, "greet_test.go", src, 0); err != nil {
		t.Fatalf("generated test does not parse: %v\n%s", err, src)
	}
}

func TestAddTestAppendsToExisting(t *testing.T) {
	addTestWriteModule(t, map[string]string{"greet.go": addTestFuncSrc})
	// First call creates the file.
	if err := addTestCommand([]string{"greet.go", "Greet"}); err != nil {
		t.Fatalf("first add-test: %v", err)
	}
	// Second call appends Add.
	if err := addTestCommand([]string{"greet.go", "Add"}); err != nil {
		t.Fatalf("second add-test: %v", err)
	}
	src := readFile(t, "greet_test.go")
	if !strings.Contains(src, "func TestGreet(") {
		t.Fatalf("TestGreet missing after append:\n%s", src)
	}
	if !strings.Contains(src, "func TestAdd(") {
		t.Fatalf("TestAdd missing after append:\n%s", src)
	}
	// Verify it still parses.
	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, "greet_test.go", src, 0); err != nil {
		t.Fatalf("file does not parse after append: %v\n%s", err, src)
	}
}

func TestAddTestDuplicateRefuses(t *testing.T) {
	addTestWriteModule(t, map[string]string{"greet.go": addTestFuncSrc})
	if err := addTestCommand([]string{"greet.go", "Greet"}); err != nil {
		t.Fatalf("first add-test: %v", err)
	}
	err := addTestCommand([]string{"greet.go", "Greet"})
	assertExitCode(t, err, exitNotFound)
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("error should mention already exists, got: %v", err)
	}
}

func TestAddTestMissingFuncError(t *testing.T) {
	addTestWriteModule(t, map[string]string{"greet.go": addTestFuncSrc})
	err := addTestCommand([]string{"greet.go", "NoSuchFunc"})
	if err == nil {
		t.Fatal("expected error for missing function")
	}
}

func TestAddTestNoopFunction(t *testing.T) {
	addTestWriteModule(t, map[string]string{"greet.go": addTestFuncSrc})
	if err := addTestCommand([]string{"greet.go", "Noop"}); err != nil {
		t.Fatalf("add-test Noop: %v", err)
	}
	src := readFile(t, "greet_test.go")
	if !strings.Contains(src, "func TestNoop(") {
		t.Fatalf("TestNoop not found:\n%s", src)
	}
	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, "greet_test.go", src, 0); err != nil {
		t.Fatalf("generated TestNoop does not parse: %v\n%s", err, src)
	}
}

func TestAddTestWithErrorReturn(t *testing.T) {
	addTestWriteModule(t, map[string]string{"lookup.go": addTestWithErrorSrc})
	if err := addTestCommand([]string{"lookup.go", "Lookup"}); err != nil {
		t.Fatalf("add-test Lookup: %v", err)
	}
	src := readFile(t, "lookup_test.go")
	if !strings.Contains(src, "wantErr") {
		t.Fatalf("wantErr field not found for error return:\n%s", src)
	}
	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, "lookup_test.go", src, 0); err != nil {
		t.Fatalf("generated test does not parse: %v\n%s", err, src)
	}
}

func TestAddTestDryRunDoesNotWrite(t *testing.T) {
	addTestWriteModule(t, map[string]string{"greet.go": addTestFuncSrc})
	if err := addTestCommand([]string{"greet.go", "Greet", "--dry-run"}); err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if _, err := os.Stat("greet_test.go"); !os.IsNotExist(err) {
		t.Fatal("--dry-run must not create the test file")
	}
}

func TestAddTestJSONOutput(t *testing.T) {
	addTestWriteModule(t, map[string]string{"greet.go": addTestFuncSrc})
	out := captureStdout(t, func() {
		if err := addTestCommand([]string{"greet.go", "Greet", "--json"}); err != nil {
			t.Errorf("--json: %v", err)
		}
	})
	var res mutationResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("output not valid JSON: %v\n%s", err, out)
	}
	if !res.Success {
		t.Fatalf("expected success=true, got: %+v", res)
	}
	if res.UndoToken == "" {
		t.Fatal("undo token must be set on success")
	}
}
