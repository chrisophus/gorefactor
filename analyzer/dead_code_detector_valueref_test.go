package analyzer

import (
	"os"
	"path/filepath"
	"testing"
)

// A function referenced only as a value (stored in a struct literal, never
// called) must NOT be reported dead. This is the regression: FindCallers and
// FindAllUses both miss value references, which let the autofix delete live
// command handlers.
func TestDeadCode_ValueReferencedFuncIsNotDead(t *testing.T) {
	dead := deadNames(t, map[string]string{
		"reg.go": `package p
type command struct{ run func([]string) error }
var registry = []command{{run: handler}}
func handler(args []string) error { return nil }
`,
	})
	if dead["handler"] {
		t.Errorf("handler is referenced as a value in registry; must not be flagged dead")
	}
}

// A method invoked only via selector must not be reported dead.
func TestDeadCode_SelectorCalledMethodIsNotDead(t *testing.T) {
	dead := deadNames(t, map[string]string{
		"m.go": `package p
type box struct{}
func (b box) describe() string { return "x" }
func use() string { var b box; return b.describe() }
`,
	})
	if dead["describe"] {
		t.Errorf("describe is called via selector; must not be flagged dead")
	}
}

// A genuinely unreferenced unexported function MUST still be reported dead,
// proving the conservative check did not silence the rule entirely.
func TestDeadCode_TrulyUnusedFuncIsDead(t *testing.T) {
	dead := deadNames(t, map[string]string{
		"o.go": `package p
func orphan() int { return 42 }
func Live() {}
`,
	})
	if !dead["orphan"] {
		t.Errorf("orphan is never referenced; expected it to be flagged dead")
	}
}

// writeTempPkg writes the given files into a temp dir and returns their paths.
func writeTempPkg(t *testing.T, files map[string]string) []string {
	t.Helper()
	dir := t.TempDir()
	var paths []string
	for name, src := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	return paths
}

func deadNames(t *testing.T, files map[string]string) map[string]bool {
	t.Helper()
	paths := writeTempPkg(t, files)
	got := map[string]bool{}
	for _, iss := range NewDeadCodeDetector(paths).DetectDeadFunctions() {
		got[iss.Name] = true
	}
	return got
}
