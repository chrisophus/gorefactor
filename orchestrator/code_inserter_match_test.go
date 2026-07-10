package orchestrator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const nestedMatchSrc = `package main

import "fmt"

func Process(items []string) error {
	total := 0
	for _, item := range items {
		if item == "" {
			continue
		}
		fmt.Println("processing", item)
		total++
	}
	fmt.Println("done", total)
	return nil
}

func register() {
	addCommand(command{
		name:        "doctor",
		description: "Aggregate health gate: lint + build + test.",
	})
}

type command struct {
	name        string
	description string
}

func addCommand(c command) {}
`

func writeMatchFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "main.go")
	if err := os.WriteFile(path, []byte(nestedMatchSrc), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestReplaceCodeBlockNestedStatement locks in that a complete statement
// nested inside a loop is replaced in place — not its enclosing loop. The
// old substring matcher replaced the whole top-level statement containing
// the pattern, silently destroying the surrounding code.
func TestReplaceCodeBlockNestedStatement(t *testing.T) {
	path := writeMatchFixture(t)
	ci := NewCodeInserter()
	loc := &InsertionLocation{Type: "inside_function", FunctionName: "Process"}
	if _, err := ci.ReplaceCodeBlock(path, loc, `fmt.Println("processing", item)`, `fmt.Printf("processing %s\n", item)`); err != nil {
		t.Fatalf("ReplaceCodeBlock: %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	src := string(content)
	if !strings.Contains(src, "for _, item := range items {") {
		t.Errorf("enclosing loop must survive a nested replace:\n%s", src)
	}
	if !strings.Contains(src, `fmt.Printf("processing %s\n", item)`) {
		t.Errorf("replacement missing:\n%s", src)
	}
	if !strings.Contains(src, "total++") {
		t.Errorf("sibling statements in the loop must survive:\n%s", src)
	}
}

// TestReplaceCodeBlockRejectsFragment locks in that a pattern that is only a
// fragment of a statement (here: a string literal inside a composite
// literal) is a not-found, never a match of the enclosing statement.
func TestReplaceCodeBlockRejectsFragment(t *testing.T) {
	path := writeMatchFixture(t)
	ci := NewCodeInserter()
	loc := &InsertionLocation{Type: "inside_function", FunctionName: "register"}
	_, err := ci.ReplaceCodeBlock(path, loc,
		`"Aggregate health gate: lint + build + test."`,
		`"Aggregate health gate: lint + golangci + build + test."`)
	if err == nil || !strings.Contains(err.Error(), "no statement matching") {
		t.Fatalf("fragment pattern must be a not-found, got err=%v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "addCommand(command{") {
		t.Errorf("file must be untouched after a failed match:\n%s", content)
	}
}

// TestRemoveCodeBlockNestedStatement: same guarantee for removal.
func TestRemoveCodeBlockNestedStatement(t *testing.T) {
	path := writeMatchFixture(t)
	ci := NewCodeInserter()
	loc := &InsertionLocation{Type: "inside_function", FunctionName: "Process"}
	if _, err := ci.RemoveCodeBlock(path, loc, `total++`); err != nil {
		t.Fatalf("RemoveCodeBlock: %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	src := string(content)
	if strings.Contains(src, "total++") {
		t.Errorf("statement should be removed:\n%s", src)
	}
	if !strings.Contains(src, `fmt.Println("processing", item)`) || !strings.Contains(src, "for _, item := range items {") {
		t.Errorf("surrounding loop must survive:\n%s", src)
	}
}
