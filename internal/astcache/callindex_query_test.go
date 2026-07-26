package astcache

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/chrisophus/gorefactor/internal/cerr"
)

// querySrc is a fixture exercising every call-edge resolution rule: plain
// calls, method calls, a name shared by a function and a method, duplicate
// calls, fan-out ordering, nested calls, and a two-function cycle.
const querySrc = `package p

type S struct{}
type T struct{}

func (s S) Ping() {}
func (t T) Ping() {}
func (s S) Solo() {}
func (t T) Shadow() {}

func Shadow() {}

func Top() { Mid() }
func Mid() { Leaf() }
func Leaf() {}

func Dup() { Leaf(); Leaf() }
func Fan() { Mid(); Leaf() }

func UseMethod(s S) { s.Solo() }
func CallShadow() { Shadow() }
func CallSel(t T) { t.Shadow() }

func Nested() {
	if len("x") > 0 {
		Leaf()
	}
}

func CycA() { CycB() }
func CycB() { CycA() }

func Inner() int { return 0 }
func Discard(x int) {}
func Wrap() { Discard(Inner()) }
`

func TestCgDefKey(t *testing.T) {
	m := &CgDef{Name: "M", Receiver: "R"}
	if got := m.Key(); got != "R:M" {
		t.Errorf("method key: want \"R:M\", got %q", got)
	}
	f := &CgDef{Name: "F"}
	if got := f.Key(); got != "F" {
		t.Errorf("function key: want \"F\", got %q", got)
	}
}

func TestLookupSemantics(t *testing.T) {
	idx := buildQueryIndex(t)

	// Explicit receiver selects the exact method even when the name is shared.
	if d := idx.Lookup("Ping", "T"); d == nil || d.Receiver != "T" {
		t.Errorf("Lookup(Ping, T): want method on T, got %+v", d)
	}
	if d := idx.Lookup("Ping", "S"); d == nil || d.Receiver != "S" {
		t.Errorf("Lookup(Ping, S): want method on S, got %+v", d)
	}

	// Bare name resolves a plain function first, even when a same-named
	// method exists.
	if d := idx.Lookup("Shadow", ""); d == nil || d.Receiver != "" {
		t.Errorf("Lookup(Shadow): want the plain function, got %+v", d)
	}

	// Bare name falls back to a unique method.
	if d := idx.Lookup("Solo", ""); d == nil || d.Receiver != "S" {
		t.Errorf("Lookup(Solo): want unique method S:Solo, got %+v", d)
	}

	// Ambiguous bare method name resolves to nothing.
	if d := idx.Lookup("Ping", ""); d != nil {
		t.Errorf("Lookup(Ping) is ambiguous: want nil, got %+v", d)
	}

	if d := idx.Lookup("Nope", ""); d != nil {
		t.Errorf("Lookup(Nope): want nil, got %+v", d)
	}
}

func TestLookupTargetOrSuggest(t *testing.T) {
	idx := buildQueryIndex(t)

	def, err := idx.LookupTargetOrSuggest("T:Ping")
	if err != nil || def == nil || def.Receiver != "T" || def.Name != "Ping" {
		t.Fatalf("LookupTargetOrSuggest(T:Ping): want T:Ping, got %+v err=%v", def, err)
	}
	def, err = idx.LookupTargetOrSuggest("Top")
	if err != nil || def == nil || def.Name != "Top" {
		t.Fatalf("LookupTargetOrSuggest(Top): want Top, got %+v err=%v", def, err)
	}

	// Missing target: exit-2 error carrying every def key as a candidate
	// (small index, so no truncation).
	def, err = idx.LookupTargetOrSuggest("Missing")
	if def != nil || err == nil {
		t.Fatalf("LookupTargetOrSuggest(Missing): want (nil, error), got %+v err=%v", def, err)
	}
	var ce *cerr.CLIError
	if !errors.As(err, &ce) {
		t.Fatalf("error is not a *cerr.CLIError: %v", err)
	}
	if len(ce.Cands) != len(idx.Defs) {
		t.Errorf("candidates: want all %d defs, got %d", len(idx.Defs), len(ce.Cands))
	}
	for i := 1; i < len(ce.Cands); i++ {
		if ce.Cands[i-1] > ce.Cands[i] {
			t.Errorf("candidates not sorted: %q before %q", ce.Cands[i-1], ce.Cands[i])
		}
	}
}

// TestLookupTargetOrSuggestTruncatesCandidates pins the 30-candidate cap on
// the not-found listing.
func TestLookupTargetOrSuggestTruncatesCandidates(t *testing.T) {
	var b strings.Builder
	b.WriteString("package big\n\n")
	for i := 0; i < 35; i++ {
		fmt.Fprintf(&b, "func F%02d() {}\n", i)
	}
	dir := t.TempDir()
	f := writeTempGo(t, dir, "big.go", b.String())
	idx, err := NewCallIndexCache().BuildWith(NewParseCache(), []string{f})
	if err != nil {
		t.Fatal(err)
	}
	_, err = idx.LookupTargetOrSuggest("Missing")
	var ce *cerr.CLIError
	if !errors.As(err, &ce) {
		t.Fatalf("error is not a *cerr.CLIError: %v", err)
	}
	if len(ce.Cands) != 30 {
		t.Errorf("candidates: want cap of 30, got %d", len(ce.Cands))
	}
}

func TestBuildTreeDepthAndDirection(t *testing.T) {
	idx := buildQueryIndex(t)
	top := idx.Defs["Top"]
	leaf := idx.Defs["Leaf"]

	// Depth 0: just the node, no expansion.
	tree := idx.BuildTree(top, "callees", 0, map[string]bool{top.Key(): true})
	if tree.Name != "Top" || len(tree.Children) != 0 {
		t.Errorf("depth 0: want bare Top node, got %d children", len(tree.Children))
	}

	// Depth 1: one level of callees, unexpanded.
	tree = idx.BuildTree(top, "callees", 1, map[string]bool{top.Key(): true})
	if len(tree.Children) != 1 || tree.Children[0].Name != "Mid" {
		t.Fatalf("depth 1: want single child Mid, got %+v", tree.Children)
	}
	if len(tree.Children[0].Children) != 0 {
		t.Errorf("depth 1: Mid must not be expanded, got %d children", len(tree.Children[0].Children))
	}

	// Depth 2: Mid expands to Leaf.
	tree = idx.BuildTree(top, "callees", 2, map[string]bool{top.Key(): true})
	mid := tree.Children[0]
	if len(mid.Children) != 1 || mid.Children[0].Name != "Leaf" {
		t.Errorf("depth 2: want Mid -> Leaf, got %+v", mid.Children)
	}

	// Callers direction: Leaf is called by Dup, Fan, Mid, Nested (sorted).
	tree = idx.BuildTree(leaf, "callers", 1, map[string]bool{leaf.Key(): true})
	var names []string
	for _, c := range tree.Children {
		names = append(names, c.Name)
	}
	want := []string{"Dup", "Fan", "Mid", "Nested"}
	if !eqStrings(names, want) {
		t.Errorf("callers of Leaf: want %v, got %v", want, names)
	}

	// Callees direction on a leaf: nothing.
	tree = idx.BuildTree(leaf, "callees", 1, map[string]bool{leaf.Key(): true})
	if len(tree.Children) != 0 {
		t.Errorf("callees of Leaf: want none, got %+v", tree.Children)
	}
}

func TestBuildTreeMarksCycles(t *testing.T) {
	idx := buildQueryIndex(t)
	a := idx.Defs["CycA"]

	tree := idx.BuildTree(a, "callees", 5, map[string]bool{a.Key(): true})
	if len(tree.Children) != 1 || tree.Children[0].Name != "CycB" {
		t.Fatalf("want CycA -> CycB, got %+v", tree.Children)
	}
	b := tree.Children[0]
	if b.Cycle {
		t.Errorf("CycB on first visit must not be marked cycle")
	}
	if len(b.Children) != 1 || b.Children[0].Name != "CycA" {
		t.Fatalf("want CycB -> CycA, got %+v", b.Children)
	}
	back := b.Children[0]
	if !back.Cycle {
		t.Errorf("revisited CycA must be marked cycle")
	}
	if len(back.Children) != 0 {
		t.Errorf("cycle node must not be expanded, got %d children", len(back.Children))
	}
}

func TestTransitiveCallers(t *testing.T) {
	idx := buildQueryIndex(t)

	var got []string
	for _, d := range idx.TransitiveCallers(idx.Defs["Leaf"]) {
		got = append(got, d.Key())
	}
	sorted := append([]string(nil), got...)
	sort.Strings(sorted)
	want := []string{"Dup", "Fan", "Mid", "Nested", "Top"}
	if !eqStrings(sorted, want) {
		t.Errorf("transitive callers of Leaf: want %v, got %v", want, sorted)
	}

	// Cycles terminate and exclude the root itself.
	var cyc []string
	for _, d := range idx.TransitiveCallers(idx.Defs["CycA"]) {
		cyc = append(cyc, d.Key())
	}
	if !eqStrings(cyc, []string{"CycB"}) {
		t.Errorf("transitive callers of CycA: want [CycB], got %v", cyc)
	}

	if out := idx.TransitiveCallers(idx.Defs["Top"]); len(out) != 0 {
		t.Errorf("transitive callers of Top: want none, got %v", out)
	}
}

func TestEdgeResolution(t *testing.T) {
	idx := buildQueryIndex(t)
	edges := cgIndexEdges(idx)

	// Selector call resolves to the method.
	if !contains(edges, "UseMethod->S:Solo") {
		t.Errorf("missing selector edge UseMethod->S:Solo in %v", edges)
	}
	// Ident call resolves to the plain function only, never a same-named method.
	if !contains(edges, "CallShadow->Shadow") {
		t.Errorf("missing ident edge CallShadow->Shadow in %v", edges)
	}
	if contains(edges, "CallShadow->T:Shadow") {
		t.Errorf("ident call must not match method T:Shadow: %v", edges)
	}
	// Selector call on a shared name matches the plain function (package-
	// qualified form) plus same-named methods — and nothing else.
	var callSel []string
	for _, d := range idx.Callees["CallSel"] {
		callSel = append(callSel, d.Key())
	}
	if !eqStrings(callSel, []string{"Shadow", "T:Shadow"}) {
		t.Errorf("CallSel callees: want [Shadow T:Shadow], got %v", callSel)
	}

	// Duplicate calls collapse to one edge.
	if n := len(idx.Callees["Dup"]); n != 1 {
		t.Errorf("Dup callees: want 1 deduped edge, got %d", n)
	}

	// Adjacency lists are sorted by key for deterministic output.
	var fan []string
	for _, d := range idx.Callees["Fan"] {
		fan = append(fan, d.Key())
	}
	if !eqStrings(fan, []string{"Leaf", "Mid"}) {
		t.Errorf("Fan callees: want sorted [Leaf Mid], got %v", fan)
	}

	// Calls nested inside statements are found.
	if !contains(edges, "Nested->Leaf") {
		t.Errorf("missing nested call edge Nested->Leaf in %v", edges)
	}

	// Calls nested inside another call expression's arguments are found:
	// the AST walk must descend into CallExpr children.
	if !contains(edges, "Wrap->Inner") {
		t.Errorf("missing argument-nested call edge Wrap->Inner in %v", edges)
	}
	if !contains(edges, "Wrap->Discard") {
		t.Errorf("missing edge Wrap->Discard in %v", edges)
	}
}

// TestBuildWithSkipsBadFiles pins the best-effort contract: missing and
// unparseable files are skipped, everything else still indexes.
func TestBuildWithSkipsBadFiles(t *testing.T) {
	dir := t.TempDir()
	good := writeTempGo(t, dir, "good.go", "package p\n\nfunc Good() {}\n")
	bad := writeTempGo(t, dir, "bad.go", "package p\n\nfunc {\n")
	missing := dir + "/does-not-exist.go"

	idx, err := NewCallIndexCache().BuildWith(NewParseCache(), []string{good, bad, missing})
	if err != nil {
		t.Fatalf("BuildWith with bad files must not error: %v", err)
	}
	if _, ok := idx.Defs["Good"]; !ok {
		t.Errorf("good file not indexed; defs: %v", cgIndexDefs(idx))
	}
	if len(idx.Defs) != 1 {
		t.Errorf("want exactly 1 def, got %v", cgIndexDefs(idx))
	}
}

func TestParseCacheKeysByAbsolutePath(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()

	a := writeTempGo(t, dirA, "x.go", "package aaa\n\nfunc FromA() {}\n")
	b := writeTempGo(t, dirB, "x.go", "package bbb\n\nfunc FromB() {}\n")
	now := time.Now().Add(-time.Minute)
	if err := os.Chtimes(a, now, now); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(b, now, now); err != nil {
		t.Fatal(err)
	}

	pc := NewParseCache()
	t.Chdir(dirA)
	_, asts := pc.Load([]string{"x.go"})
	if got := asts["x.go"]; got == nil || got.Name.Name != "aaa" {
		t.Fatalf("load from dirA: want package aaa, got %v", got)
	}
	t.Chdir(dirB)
	_, asts = pc.Load([]string{"x.go"})
	if got := asts["x.go"]; got == nil || got.Name.Name != "bbb" {
		t.Errorf("load from dirB returned dirA's cached AST: cache keyed by relative path")
	}
}

// buildQueryIndex builds a throwaway index over querySrc, touching no global
// caches so tests stay independent.
func buildQueryIndex(t *testing.T) *CgIndex {
	t.Helper()
	dir := t.TempDir()
	f := writeTempGo(t, dir, "q.go", querySrc)
	idx, err := NewCallIndexCache().BuildWith(NewParseCache(), []string{f})
	if err != nil {
		t.Fatalf("BuildWith: %v", err)
	}
	return idx
}
