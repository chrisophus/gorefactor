package astcache

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/chrisophus/gorefactor/analyzer"
)

// TestCallIndexCacheMatchesUncached is the core equivalence guarantee: the
// cached builder must produce exactly the same defs and edges as a cold,
// throwaway build over a real, multi-file package.
func TestCallIndexCacheMatchesUncached(t *testing.T) {
	files := collectFiles(t, "../../analyzer")
	cached, err := BuildCallIndex(files)
	if err != nil {
		t.Fatalf("cached build: %v", err)
	}
	cold, err := buildCallIndexUncached(files)
	if err != nil {
		t.Fatalf("uncached build: %v", err)
	}
	if !eqStrings(cgIndexDefs(cached), cgIndexDefs(cold)) {
		t.Errorf("defs differ between cached and uncached build")
	}
	if !eqStrings(cgIndexEdges(cached), cgIndexEdges(cold)) {
		t.Errorf("edges differ between cached and uncached build")
	}
}

// TestParseCacheReusesUnchangedFiles asserts the parse cache only parses each
// file once while it is unchanged, then re-parses exactly the file that changed.
func TestParseCacheReusesUnchangedFiles(t *testing.T) {
	dir := t.TempDir()
	a := writeTempGo(t, dir, "a.go", "package p\n\nfunc A() { B() }\n")
	b := writeTempGo(t, dir, "b.go", "package p\n\nfunc B() {}\n")
	files := []string{a, b}

	pc := NewParseCache()
	pc.Load(files)
	if got := pc.ParseCount(); got != 2 {
		t.Fatalf("cold load: want 2 parses, got %d", got)
	}
	pc.Load(files)
	if got := pc.ParseCount(); got != 2 {
		t.Fatalf("warm load re-parsed unchanged files: parse count rose to %d", got)
	}

	// Change one file (different size guarantees a new fingerprint); bump mtime
	// too so the result holds even on coarse-granularity filesystems.
	writeTempGo(t, dir, "a.go", "package p\n\nfunc A() { B(); B() }\n")
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(a, future, future); err != nil {
		t.Fatal(err)
	}
	pc.Load(files)
	if got := pc.ParseCount(); got != 3 {
		t.Fatalf("after editing one file, want exactly 1 re-parse (total 3), got %d", got)
	}
}

// TestCallIndexCacheInvalidatesOnChange checks that editing a file changes the
// resulting edges and that the cached result still matches a cold rebuild.
func TestCallIndexCacheInvalidatesOnChange(t *testing.T) {
	dir := t.TempDir()
	a := writeTempGo(t, dir, "a.go", "package p\n\nfunc A() { B() }\nfunc B() {}\nfunc C() {}\n")
	files := []string{a}

	pc := NewParseCache()
	cc := NewCallIndexCache()
	idx1, err := cc.BuildWith(pc, files)
	if err != nil {
		t.Fatal(err)
	}
	if got := cc.ExtractCount(); got != 1 {
		t.Fatalf("cold build: want 1 extraction, got %d", got)
	}
	edges1 := cgIndexEdges(idx1)
	if !contains(edges1, "A->B") || contains(edges1, "A->C") {
		t.Fatalf("unexpected initial edges: %v", edges1)
	}

	// A now also calls C.
	writeTempGo(t, dir, "a.go", "package p\n\nfunc A() { B(); C() }\nfunc B() {}\nfunc C() {}\n")
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(a, future, future); err != nil {
		t.Fatal(err)
	}

	idx2, err := cc.BuildWith(pc, files)
	if err != nil {
		t.Fatal(err)
	}
	if got := cc.ExtractCount(); got != 2 {
		t.Fatalf("after edit: want 1 more extraction (total 2), got %d", got)
	}
	edges2 := cgIndexEdges(idx2)
	if !contains(edges2, "A->C") {
		t.Fatalf("edit not reflected; edges: %v", edges2)
	}

	// Cached result must equal a fully cold rebuild of the new state.
	cold, err := buildCallIndexUncached(files)
	if err != nil {
		t.Fatal(err)
	}
	if !eqStrings(edges2, cgIndexEdges(cold)) {
		t.Errorf("cached edges after edit differ from cold rebuild\ncached: %v\ncold:   %v", edges2, cgIndexEdges(cold))
	}
}

// TestCallIndexCacheWarmSkipsExtraction asserts a second build with no file
// changes reuses cached per-file data and extracts nothing new.
func TestCallIndexCacheWarmSkipsExtraction(t *testing.T) {
	dir := t.TempDir()
	a := writeTempGo(t, dir, "a.go", "package p\n\nfunc A() { B() }\nfunc B() {}\n")
	files := []string{a}

	pc := NewParseCache()
	cc := NewCallIndexCache()
	idx1, _ := cc.BuildWith(pc, files)
	idx2, _ := cc.BuildWith(pc, files)
	if got := cc.ExtractCount(); got != 1 {
		t.Fatalf("warm rebuild re-extracted: extraction count is %d, want 1", got)
	}
	if !eqStrings(cgIndexEdges(idx1), cgIndexEdges(idx2)) {
		t.Errorf("warm rebuild produced different edges")
	}
}

// BenchmarkBuildCallIndexCold rebuilds the index from scratch every iteration
// (fresh caches) — the per-invocation cost a long-lived server pays without
// Phase 2.
func BenchmarkBuildCallIndexCold(b *testing.B) {
	files := collectFiles(b, "../../analyzer")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := buildCallIndexUncached(files); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkBuildCallIndexWarm reuses the process-global cache across iterations,
// as the MCP server does — files are stat'd but never re-parsed or re-walked.
func BenchmarkBuildCallIndexWarm(b *testing.B) {
	files := collectFiles(b, "../../analyzer")
	resetIndexCaches()
	if _, err := BuildCallIndex(files); err != nil { // prime
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := BuildCallIndex(files); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkFindCallersCold runs find-callers with a throwaway parse cache each
// iteration (the cost without Phase 2).
func BenchmarkFindCallersCold(b *testing.B) {
	files := collectFiles(b, "../../analyzer")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pc := NewParseCache()
		ca := analyzer.NewCallAnalyzer(files)
		ca.SeedASTs(pc.Load(files))
		if _, err := ca.FindCallers("NewUseAnalyzer", ""); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkFindCallersWarm reuses the global parse cache, so the per-query work
// is only definition/use collection over already-parsed ASTs.
func BenchmarkFindCallersWarm(b *testing.B) {
	files := collectFiles(b, "../../analyzer")
	resetIndexCaches()
	{
		ca := analyzer.NewCallAnalyzer(files)
		ca.SeedASTs(GlobalParseCache.Load(files)) // prime
		_, _ = ca.FindCallers("NewUseAnalyzer", "")
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ca := analyzer.NewCallAnalyzer(files)
		ca.SeedASTs(GlobalParseCache.Load(files))
		if _, err := ca.FindCallers("NewUseAnalyzer", ""); err != nil {
			b.Fatal(err)
		}
	}
}

// collectFiles gathers the Go files under root using the analyzer's walker,
// skipping the test when there is nothing to index.
func collectFiles(tb testing.TB, root string) []string {
	tb.Helper()
	files, err := analyzer.WalkGoFiles(root, analyzer.DefaultWalkOptions())
	if err != nil || len(files) == 0 {
		tb.Skipf("no files under %s: %v", root, err)
	}
	return files
}

func writeTempGo(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

// cgIndexEdges renders an index's call edges as sorted "caller->callee" keys,
// a representation that is independent of map order and pointer identity so two
// indexes built different ways can be compared directly.
func cgIndexEdges(idx *CgIndex) []string {
	var out []string
	for caller, callees := range idx.Callees {
		for _, c := range callees {
			out = append(out, caller+"->"+c.Key())
		}
	}
	sort.Strings(out)
	return out
}

// cgIndexDefs renders an index's definitions as sorted "key@file:line" entries.
func cgIndexDefs(idx *CgIndex) []string {
	var out []string
	for k, d := range idx.Defs {
		out = append(out, fmt.Sprintf("%s@%s:%d", k, d.File, d.Line))
	}
	sort.Strings(out)
	return out
}

func eqStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func contains(xs []string, want string) bool {
	return slices.Contains(xs, want)
}
