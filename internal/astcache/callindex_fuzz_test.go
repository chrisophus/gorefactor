package astcache

import (
	"os"
	"path/filepath"
	"testing"
)

// FuzzBuildCallIndex feeds arbitrary source through the full parse ->
// extract -> assemble pipeline. Beyond not panicking, the assembled index
// must be internally consistent: adjacency keys refer to known defs and
// every edge appears in both directions.
func FuzzBuildCallIndex(f *testing.F) {
	seeds := []string{
		"package p\n\nfunc A() { B() }\nfunc B() {}\n",
		"package p\n\ntype T struct{}\n\nfunc (t T) M() { t.M() }\n",
		"package p\n\nfunc Shadow() {}\n\ntype T struct{}\n\nfunc (t T) Shadow() {}\n\nfunc C(t T) { Shadow(); t.Shadow() }\n",
		"package p\n\nfunc () orphan() { orphan() }\n",
		"package p\n\nfunc Stub() int\n",
		"package p\n\nfunc A() { f := func() {}; f() }\n",
		"package p\n\nfunc G[T any]() { G[int]() }\n",
		"broken {",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		path := filepath.Join(t.TempDir(), "fuzz.go")
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		idx, err := NewCallIndexCache().BuildWith(NewParseCache(), []string{path})
		if err != nil {
			t.Fatalf("BuildWith must be best-effort, got error: %v", err)
		}
		for caller, callees := range idx.Callees {
			if idx.Defs[caller] == nil {
				t.Errorf("Callees key %q is not a known def", caller)
			}
			for _, callee := range callees {
				if idx.Defs[callee.Key()] == nil {
					t.Errorf("callee %q is not a known def", callee.Key())
				}
				if !containsDef(idx.Callers[callee.Key()], caller) {
					t.Errorf("edge %s->%s missing from Callers", caller, callee.Key())
				}
			}
		}
		for callee, callers := range idx.Callers {
			if idx.Defs[callee] == nil {
				t.Errorf("Callers key %q is not a known def", callee)
			}
			for _, caller := range callers {
				if !containsDef(idx.Callees[caller.Key()], callee) {
					t.Errorf("edge %s->%s missing from Callees", caller.Key(), callee)
				}
			}
		}
	})
}

// FuzzSplitNameReceiver pins the split/join round-trip for arbitrary target
// strings: joining the parts reproduces the input, and a non-empty receiver
// implies the input contained a colon.
func FuzzSplitNameReceiver(f *testing.F) {
	for _, s := range []string{"F", "T:M", ":", "::", "a:b:c", "", "T:", ":M"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		name, recv := splitNameReceiver(s)
		if recv != "" || containsColon(s) {
			if got := recv + ":" + name; got != s {
				t.Errorf("split %q = (%q, %q); join gives %q", s, name, recv, got)
			}
		} else if name != s {
			t.Errorf("colonless %q split to name %q", s, name)
		}
		if containsColon(s) != (len(s) != len(name)+len(recv)) && recv == "" {
			t.Errorf("split %q = (%q, %q): inconsistent with containsColon=%v", s, name, recv, containsColon(s))
		}
	})
}

func containsDef(defs []*CgDef, key string) bool {
	for _, d := range defs {
		if d.Key() == key {
			return true
		}
	}
	return false
}
