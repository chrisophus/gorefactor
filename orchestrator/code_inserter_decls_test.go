package orchestrator

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

// Inserting "at the beginning" of a file that has imports must land after
// the import block — Go requires imports to precede all other declarations,
// and the old behavior wrote a file that no longer parsed.
func TestInsertAtBeginningLandsAfterImports(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.go")
	src := "package p\n\nimport \"fmt\"\n\nfunc Existing() { fmt.Println(1) }\n"
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	ci := NewCodeInserter()
	_, err := ci.InsertCode(path, &InsertionLocation{Type: "at_beginning"}, "func Added() int { return 1 }")
	if err != nil {
		t.Fatalf("insert at_beginning: %v", err)
	}
	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	f, perr := parser.ParseFile(fset, path, out, 0)
	if perr != nil {
		t.Fatalf("file no longer parses after insert: %v\n%s", perr, out)
	}
	if len(f.Decls) < 3 {
		t.Fatalf("expected import + 2 funcs, got %d decls", len(f.Decls))
	}
	if gd, ok := f.Decls[0].(*ast.GenDecl); !ok || gd.Tok != token.IMPORT {
		t.Fatalf("first declaration must remain the import block, got %T", f.Decls[0])
	}
	if fn, ok := f.Decls[1].(*ast.FuncDecl); !ok || fn.Name.Name != "Added" {
		t.Fatalf("inserted func must follow the imports, got %T", f.Decls[1])
	}
}
