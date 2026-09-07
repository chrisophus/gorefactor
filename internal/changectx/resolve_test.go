package changectx

import (
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

const mappingFixture = `package fix

import "fmt"

// Kind is what a thing is.
type Kind int

const (
	// KindA is the first kind.
	KindA Kind = iota
	KindB
)

// Greet says hello.
func Greet(name string) string {
	return fmt.Sprintf("hello %s", name)
}

// Store holds greetings.
type Store struct {
	seen []string
}

// Add records a greeting.
func (s *Store) Add(name string) {
	s.seen = append(s.seen, Greet(name))
}
`

func TestDeclsInNamesAndSpans(t *testing.T) {
	decls := fixtureDecls(t)
	got := map[string]*decl{}
	for _, d := range decls {
		got[d.symbol] = d
	}
	for _, want := range []string{"Kind", "KindA", "KindB", "Greet", "Store", "Store.Add"} {
		if got[want] == nil {
			t.Fatalf("declaration %q not found; got %v", want, keysOf(got))
		}
	}
	if d := got["Store.Add"]; d.kind != "method" || d.receiver != "Store" || !d.exported {
		t.Errorf("Store.Add: kind=%q receiver=%q exported=%v", d.kind, d.receiver, d.exported)
	}
	if d := got["Greet"]; d.scope != "pkg.Greet" {
		t.Errorf("Greet scope = %q, want pkg.Greet", d.scope)
	}
	// The doc comment is part of the declaration a reviewer reads.
	if d := got["Greet"]; d.start != 14 || d.end != 17 {
		t.Errorf("Greet span = %d..%d, want 14..17", d.start, d.end)
	}
	if d := got["KindA"]; d.start != 9 || d.end != 10 {
		t.Errorf("KindA span = %d..%d, want 9..10", d.start, d.end)
	}
}

func TestDeclsOverlapMapsHunkToEnclosingDeclaration(t *testing.T) {
	decls := fixtureDecls(t)
	cases := []struct {
		line int
		want string
	}{
		{15, "Greet"}, // the body of Greet
		{14, "Greet"}, // its doc comment
		{26, "Store.Add"},
		{21, "Store"},
		{10, "KindA"},
		{11, "KindB"},
	}
	for _, tc := range cases {
		var hits []string
		for _, d := range decls {
			if d.overlaps(lineRange{start: tc.line, end: tc.line}) {
				hits = append(hits, d.symbol)
			}
		}
		if len(hits) != 1 || hits[0] != tc.want {
			t.Errorf("line %d maps to %v, want [%s]", tc.line, hits, tc.want)
		}
	}
}

func TestCountChangedCountsOnlyOverlap(t *testing.T) {
	decls := fixtureDecls(t)
	for _, d := range decls {
		if d.symbol != "Greet" {
			continue
		}
		got := d.countChanged([]lineRange{{start: 1, end: 15}, {start: 30, end: 34}})
		if got != 2 {
			t.Errorf("Greet changed lines = %d, want 2", got)
		}
	}
}

func TestMergeRangesFoldsTouchingSpans(t *testing.T) {
	got := mergeRanges([]lineRange{{10, 12}, {4, 5}, {13, 14}, {4, 4}, {20, 20}})
	want := []lineRange{{4, 5}, {10, 14}, {20, 20}}
	if len(got) != len(want) {
		t.Fatalf("merged to %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("merged to %v, want %v", got, want)
		}
	}
}

func TestParseHunkHeaderDeletionOnly(t *testing.T) {
	r, ok := parseHunkHeader("@@ -40,6 +39,0 @@ func x() {")
	if !ok || r.start != 39 || r.end != 39 {
		t.Errorf("deletion hunk parsed as %+v (ok=%v), want 39..39", r, ok)
	}
	r, ok = parseHunkHeader("@@ -1 +1 @@")
	if !ok || r.start != 1 || r.end != 1 {
		t.Errorf("single-line hunk parsed as %+v (ok=%v), want 1..1", r, ok)
	}
}

func keysOf(m map[string]*decl) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func fixtureDecls(t *testing.T) []*decl {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fix.go")
	if err := os.WriteFile(path, []byte(mappingFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	idx := &index{byAbs: map[string]*fileUnit{}, fallback: token.NewFileSet()}
	unit := idx.unit(path)
	if unit == nil || unit.syntax == nil {
		t.Fatal("fixture did not parse")
	}
	return declsIn(unit, "pkg/fix.go", "pkg")
}
