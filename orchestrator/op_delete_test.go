package orchestrator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDeleteDeclaration_RemovesDocComment is a regression test for the bug
// where gorefactor delete left the function's leading doc comment behind as a
// free-floating comment in the file.
func TestDeleteDeclaration_RemovesDocComment(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "example.go")

	src := `package example

// helper does something useful.
func helper() string {
	return "hi"
}

func main() {}
`
	if err := os.WriteFile(file, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	orch := NewOrchestrator()
	ops := []*RefactoringOperation{
		{
			Type: "delete_declaration",
			File: file,
			Target: &TargetSpecification{
				FunctionName: "helper",
			},
		},
	}
	if _, err := orch.ExecuteOperations(ops); err != nil {
		t.Fatalf("ExecuteOperations: %v", err)
	}

	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(string(got), "helper does something useful") {
		t.Errorf("doc comment was not removed:\n%s", got)
	}
	if strings.Contains(string(got), "func helper") {
		t.Errorf("function body was not removed:\n%s", got)
	}
}

// TestDeleteDeclaration_RemovesGenDeclDocComment checks that doc comments on
// type declarations are also removed.
func TestDeleteDeclaration_RemovesGenDeclDocComment(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "example.go")

	src := `package example

// MyError is an error type.
type MyError struct {
	msg string
}

func other() {}
`
	if err := os.WriteFile(file, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	orch := NewOrchestrator()
	ops := []*RefactoringOperation{
		{
			Type: "delete_declaration",
			File: file,
			Target: &TargetSpecification{
				TypeName: "MyError",
			},
		},
	}
	if _, err := orch.ExecuteOperations(ops); err != nil {
		t.Fatalf("ExecuteOperations: %v", err)
	}

	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(string(got), "MyError is an error type") {
		t.Errorf("doc comment was not removed:\n%s", got)
	}
}
