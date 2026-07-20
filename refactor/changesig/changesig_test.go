package changesig_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chrisophus/gorefactor/internal/cerr"
	"github.com/chrisophus/gorefactor/refactor/changesig"
)

const greetSrc = `package main

func Greet(name string, loud bool) string {
	if loud {
		return "HELLO " + name
	}
	return "hello " + name
}

func main() {
	_ = Greet("bob", false)
}
`

// TestPlanApplyAddParam exercises the engine end-to-end without the CLI: Plan
// computes edits, Apply writes them, and both the signature and every call site
// are updated.
func TestPlanApplyAddParam(t *testing.T) {
	writeModule(t, map[string]string{"main.go": greetSrc})
	edits, detail, err := changesig.Plan("main.go", "Greet", &changesig.Action{Kind: "add", ParamName: "count", ParamType: "int", Position: -1})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if !strings.Contains(detail, "Added parameter") {
		t.Fatalf("unexpected detail: %q", detail)
	}
	if err := changesig.Apply(edits); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	src := readFile(t, "main.go")
	if !strings.Contains(src, "func Greet(name string, loud bool, count int) string") {
		t.Fatalf("signature not updated:\n%s", src)
	}
	if !strings.Contains(src, `Greet("bob", false, 0)`) {
		t.Fatalf("call site not updated:\n%s", src)
	}
}

// TestPlanRemoveParamUsedInBodyRefuses proves the engine surfaces a semantic
// (exit-2) refusal without touching the file, matching CLI behaviour.
func TestPlanRemoveParamUsedInBodyRefuses(t *testing.T) {
	path := writeModule(t, map[string]string{"main.go": greetSrc})
	before := readFile(t, path)
	_, _, err := changesig.Plan("main.go", "Greet", &changesig.Action{Kind: "remove", RemoveRef: "loud", Position: -1})
	if err == nil {
		t.Fatal("expected refusal removing a body-used parameter")
	}
	if cerr.ExitCodeFor(err) != cerr.ExitNotFound {
		t.Fatalf("want exit %d, got %d (%v)", cerr.ExitNotFound, cerr.ExitCodeFor(err), err)
	}
	if readFile(t, path) != before {
		t.Fatal("Plan must not modify the file")
	}
}

// writeModule writes a go.mod plus files into a fresh temp dir and chdirs into
// it, returning the primary file path.
func writeModule(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module sigmod\n\ngo 1.24\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Join(dir, "main.go")
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
