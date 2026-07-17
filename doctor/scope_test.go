package doctor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestChangedScopeIncludesReverseDeps(t *testing.T) {
	root := gitRepo(t, map[string]string{
		"go.mod":      "module example.com/m\n\ngo 1.22\n",
		"core/c.go":   "package core\n\nfunc C() {}\n",
		"caller/u.go": "package caller\n\nimport \"example.com/m/core\"\n\nfunc U() { core.C() }\n",
		"other/o.go":  "package other\n\nfunc O() {}\n",
	})
	if err := os.WriteFile(filepath.Join(root, "core", "c.go"),
		[]byte("package core\n\nfunc C() {}\n\nfunc C2() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	scope, err := ChangedScope(root, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, d := range scope {
		got[d] = true
	}
	if !got["core"] {
		t.Fatalf("changed package missing from scope: %v", scope)
	}
	if !got["caller"] {
		t.Fatalf("direct reverse dependency missing from scope (depth-1): %v", scope)
	}
	if got["other"] {
		t.Fatalf("unrelated package must stay out of scope: %v", scope)
	}
}

func TestChangedScopeEmptyWhenClean(t *testing.T) {
	root := gitRepo(t, map[string]string{"a.go": "package a\n"})
	scope, err := ChangedScope(root, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if len(scope) != 0 {
		t.Fatalf("clean tree should have empty scope: %v", scope)
	}
}
