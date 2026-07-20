package orchestrator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRenameTyped_Shadowing renames a package-level var while a local variable
// of the same name shadows it inside a function. Only the package-level object
// (and its uses) must change; the shadowing local must be left alone. A
// name-string rewrite would have clobbered the local too.
func TestRenameTyped_Shadowing(t *testing.T) {
	dir := writeRenameModule(t, map[string]string{
		"src.go": `package p

var count = 0

func inc() {
	count := 5
	count++
	_ = count
}

func read() int {
	return count
}
`,
	})
	src := filepath.Join(dir, "src.go")
	runRename(t, src, "count", "total")

	got := readSrc(t, src)
	if !strings.Contains(got, "var total = 0") {
		t.Errorf("package-level var not renamed:\n%s", got)
	}
	if !strings.Contains(got, "return total") {
		t.Errorf("use of package-level var not renamed:\n%s", got)
	}
	if !strings.Contains(got, "count := 5") || !strings.Contains(got, "count++") {
		t.Errorf("shadowing local was wrongly renamed:\n%s", got)
	}
}

// TestRenameTyped_SameNamedField renames a package-level var that shares its name
// with a struct field. The field declaration and field selector must be
// untouched; only the standalone variable reference changes.
func TestRenameTyped_SameNamedField(t *testing.T) {
	dir := writeRenameModule(t, map[string]string{
		"src.go": `package p

type T struct {
	name string
}

var name = "global"

func use(t T) string {
	return t.name + name
}
`,
	})
	src := filepath.Join(dir, "src.go")
	runRename(t, src, "name", "label")

	got := readSrc(t, src)
	if !strings.Contains(got, "var label = ") {
		t.Errorf("package-level var not renamed:\n%s", got)
	}
	if !strings.Contains(got, "name string") {
		t.Errorf("struct field was wrongly renamed:\n%s", got)
	}
	if !strings.Contains(got, "t.name + label") {
		t.Errorf("expected field access preserved and var renamed:\n%s", got)
	}
}

// TestRenameTyped_CrossFileFunc renames an unexported function whose definition
// and use live in different files of the same package. Both files must be
// rewritten via object identity.
func TestRenameTyped_CrossFileFunc(t *testing.T) {
	dir := writeRenameModule(t, map[string]string{
		"a.go": `package p

func helper() int { return 1 }
`,
		"b.go": `package p

func caller() int { return helper() + 1 }
`,
	})
	runRename(t, filepath.Join(dir, "a.go"), "helper", "assist")

	a := readSrc(t, filepath.Join(dir, "a.go"))
	b := readSrc(t, filepath.Join(dir, "b.go"))
	if !strings.Contains(a, "func assist()") {
		t.Errorf("definition not renamed in a.go:\n%s", a)
	}
	if strings.Contains(a, "helper") {
		t.Errorf("stale name still present in a.go:\n%s", a)
	}
	if !strings.Contains(b, "assist() + 1") {
		t.Errorf("cross-file use not renamed in b.go:\n%s", b)
	}
	if strings.Contains(b, "helper") {
		t.Errorf("stale name still present in b.go:\n%s", b)
	}
}

// TestRenameTyped_CrossFileTypeAndVar covers cross-file rename of an unexported
// type and an unexported var, exercising the non-func declaration kinds.
func TestRenameTyped_CrossFileTypeAndVar(t *testing.T) {
	files := map[string]string{
		"a.go": `package p

type widget struct{ id int }

var registry = map[string]widget{}
`,
		"b.go": `package p

func lookup(k string) widget {
	return registry[k]
}
`,
	}
	dir := writeRenameModule(t, files)
	runRename(t, filepath.Join(dir, "a.go"), "widget", "gadget")

	a := readSrc(t, filepath.Join(dir, "a.go"))
	b := readSrc(t, filepath.Join(dir, "b.go"))
	if !strings.Contains(a, "type gadget struct") || !strings.Contains(a, "map[string]gadget") {
		t.Errorf("type not renamed across its decl/use in a.go:\n%s", a)
	}
	if !strings.Contains(b, ") gadget {") {
		t.Errorf("cross-file type use not renamed in b.go:\n%s", b)
	}

	runRename(t, filepath.Join(dir, "a.go"), "registry", "store")
	a = readSrc(t, filepath.Join(dir, "a.go"))
	b = readSrc(t, filepath.Join(dir, "b.go"))
	if !strings.Contains(a, "var store = ") {
		t.Errorf("var not renamed in a.go:\n%s", a)
	}
	if !strings.Contains(b, "return store[k]") {
		t.Errorf("cross-file var use not renamed in b.go:\n%s", b)
	}
}

// TestRenameTyped_ExportedRejected keeps the unexported-only guard as defense in
// depth: exported symbols may be referenced from packages this call never loads.
func TestRenameTyped_ExportedRejected(t *testing.T) {
	dir := writeRenameModule(t, map[string]string{
		"src.go": `package p

func Exported() {}
`,
	})
	orch := NewOrchestrator()
	res, _ := orch.ExecuteOperations([]*RefactoringOperation{{
		Type:       "rename_declaration",
		File:       filepath.Join(dir, "src.go"),
		Target:     &TargetSpecification{FunctionName: "Exported"},
		Parameters: map[string]interface{}{"newName": "Renamed"},
	}})
	if res != nil && res.Success {
		t.Fatal("exported rename should be rejected by the defense-in-depth guard")
	}
}

// writeRenameModule writes a minimal Go module (go.mod + the given files) into a
// throw-away directory and returns the directory. Types-aware rename loads the
// package with go/packages, which requires a module context.
func writeRenameModule(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module renametest\n\ngo 1.21\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func runRename(t *testing.T, file, oldName, newName string) {
	t.Helper()
	orch := NewOrchestrator()
	res, err := orch.ExecuteOperations([]*RefactoringOperation{{
		Type:       "rename_declaration",
		File:       file,
		Target:     &TargetSpecification{FunctionName: oldName},
		Parameters: map[string]interface{}{"newName": newName},
	}})
	if err != nil {
		t.Fatalf("ExecuteOperations(%s->%s): %v", oldName, newName, err)
	}
	if res != nil && !res.Success {
		t.Fatalf("rename %s->%s failed: %v", oldName, newName, res.Errors)
	}
}

func readSrc(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
