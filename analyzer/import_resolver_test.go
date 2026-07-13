package analyzer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewImportResolver_CreatesInstance(t *testing.T) {
	t.Parallel()
	ir := NewImportResolver()
	if ir == nil {
		t.Fatal("NewImportResolver returned nil")
	}
}

func TestResolveImportsForMove_FunctionUsesFmt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	srcPath := writeGoFile(t, dir, "src.go", `package mypkg

import "fmt"

func Greet(name string) string {
	return fmt.Sprintf("hello %s", name)
}
`)
	// dest does not have fmt imported yet.
	destPath := writeGoFile(t, dir, "dest.go", `package mypkg
`)

	ir := NewImportResolver()
	changes, err := ir.ResolveImportsForMove(srcPath, destPath, "Greet")
	if err != nil {
		t.Fatalf("ResolveImportsForMove: %v", err)
	}
	if len(changes) == 0 {
		t.Fatal("expected at least one import change (add fmt), got none")
	}
	found := false
	for _, c := range changes {
		if c.Type == "add" && c.ImportPath == "fmt" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'add fmt' change, got: %+v", changes)
	}
}

func TestResolveImportsForMove_DestAlreadyHasImport(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	srcPath := writeGoFile(t, dir, "src2.go", `package mypkg

import "fmt"

func Print(s string) {
	fmt.Println(s)
}
`)
	// dest already imports fmt.
	destPath := writeGoFile(t, dir, "dest2.go", `package mypkg

import "fmt"

func Existing() { fmt.Println("hi") }
`)

	ir := NewImportResolver()
	changes, err := ir.ResolveImportsForMove(srcPath, destPath, "Print")
	if err != nil {
		t.Fatalf("ResolveImportsForMove: %v", err)
	}
	for _, c := range changes {
		if c.ImportPath == "fmt" {
			t.Errorf("fmt should not be added because dest already imports it; got change: %+v", c)
		}
	}
}

func TestResolveImportsForMove_FunctionNotFound(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	srcPath := writeGoFile(t, dir, "src3.go", `package mypkg

func Exists() {}
`)
	destPath := writeGoFile(t, dir, "dest3.go", `package mypkg
`)

	ir := NewImportResolver()
	_, err := ir.ResolveImportsForMove(srcPath, destPath, "DoesNotExist")
	if err == nil {
		t.Fatal("expected error for non-existent function")
	}
}

func TestResolveImportsForMove_NoImportsNeeded(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	srcPath := writeGoFile(t, dir, "src4.go", `package mypkg

func Pure(x int) int {
	return x * 2
}
`)
	destPath := writeGoFile(t, dir, "dest4.go", `package mypkg
`)

	ir := NewImportResolver()
	changes, err := ir.ResolveImportsForMove(srcPath, destPath, "Pure")
	if err != nil {
		t.Fatalf("ResolveImportsForMove: %v", err)
	}
	if len(changes) != 0 {
		t.Errorf("expected no import changes for pure function, got: %+v", changes)
	}
}

func TestNeedToRemoveImport_UsedImport(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	filePath := writeGoFile(t, dir, "used.go", `package mypkg

import "fmt"

func F() { fmt.Println("hi") }
`)

	ir := NewImportResolver()
	need, err := ir.NeedToRemoveImport(filePath, "fmt")
	if err != nil {
		t.Fatalf("NeedToRemoveImport: %v", err)
	}
	if need {
		t.Error("expected NeedToRemoveImport=false when fmt is used, got true")
	}
}

func TestNeedToRemoveImport_UnusedImport(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// File imports "os" but never uses it.
	filePath := writeGoFile(t, dir, "unused.go", `package mypkg

import "os"

func F() { return }

var _ = os.Args
`)
	// "errors" is imported but not referenced at all.
	filePath2 := writeGoFile(t, dir, "unused2.go", `package mypkg

import "errors"

func G() {}
`)

	ir := NewImportResolver()

	// os IS used (via os.Args), so NeedToRemove should be false.
	need, err := ir.NeedToRemoveImport(filePath, "os")
	if err != nil {
		t.Fatalf("NeedToRemoveImport: %v", err)
	}
	if need {
		t.Error("expected false for used import 'os'")
	}

	// errors is NOT used; NeedToRemove should be true.
	need2, err := ir.NeedToRemoveImport(filePath2, "errors")
	if err != nil {
		t.Fatalf("NeedToRemoveImport for errors: %v", err)
	}
	if !need2 {
		t.Error("expected true for unused import 'errors'")
	}
}

func TestApplyImportChanges_EmptyChanges(t *testing.T) {
	t.Parallel()
	ir := NewImportResolver()
	// Should not error with an empty list.
	if err := ir.ApplyImportChanges(nil); err != nil {
		t.Errorf("ApplyImportChanges(nil): %v", err)
	}
}

func TestApplyImportChanges_AddAndRemove(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	filePath := writeGoFile(t, dir, "change.go", `package mypkg
`)

	ir := NewImportResolver()
	changes := []ImportChange{
		{Type: "add", ImportPath: "fmt", File: filePath, Reason: "test"},
		{Type: "remove", ImportPath: "os", File: filePath, Reason: "test"},
	}
	// addImport / removeImport are stubs that return nil; this exercises the
	// dispatch logic in ApplyImportChanges without requiring real AST rewriting.
	if err := ir.ApplyImportChanges(changes); err != nil {
		t.Errorf("ApplyImportChanges: %v", err)
	}
}

func TestGetExistingImports_FileWithImports(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	filePath := writeGoFile(t, dir, "imports.go", `package mypkg

import (
	"fmt"
	"os"
	"strings"
)

func F() { _, _ = fmt.Println, os.Args; _ = strings.Join }
`)

	ir := NewImportResolver()
	imports := ir.getExistingImports(filePath)
	for _, want := range []string{"fmt", "os", "strings"} {
		if !imports[want] {
			t.Errorf("expected import %q to be present, got: %v", want, imports)
		}
	}
}

func TestGetExistingImports_MissingFile(t *testing.T) {
	t.Parallel()
	ir := NewImportResolver()
	imports := ir.getExistingImports("/nonexistent/path.go")
	if len(imports) != 0 {
		t.Errorf("expected empty imports for missing file, got %v", imports)
	}
}

func writeGoFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}
