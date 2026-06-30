package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sync"
)

// Long-lived index caches (Phase 2 of docs/mcp-server-plan.md).
//
// One-shot CLI invocations parse every file and rebuild the call index on each
// run, which is fine because the process exits immediately. The `gorefactor
// mcp` server, however, is long-lived: it answers many tool calls in one
// process, and re-reading + re-parsing the whole module on every call is pure
// waste. These caches make the server "index once": files are parsed a single
// time into a shared FileSet, and only files whose mtime/size changed are
// re-parsed on subsequent calls.
//
// Two layers, both process-global so they persist across MCP tool calls:
//
//   - parseCache:     path -> parsed *ast.File, keyed by a cheap fingerprint.
//                     Shared by the call-graph builder AND the find-* analyzers
//                     (via SeedASTs), so a find-callers call reuses ASTs a prior
//                     callgraph call already parsed.
//   - callIndexCache: path -> per-file defs + raw calls. Lets buildCallIndex
//                     skip the AST walk for unchanged files and only re-run the
//                     (cheap) cross-file edge resolution.
//
// Invalidation is by file content fingerprint (mtime + size), the same signal
// the Go toolchain and gopls use. The file *set* is NOT cached: callers re-walk
// the tree each call (collectGoFiles), so added/removed files are always seen;
// the caches only memoize the expensive per-file parse/extract step.

// fileFingerprint is a cheap content-version signal: modification time (in
// nanoseconds) plus size. If either differs, the file is treated as changed and
// re-parsed. This mirrors the build cache / gopls approach and avoids hashing
// file contents on every call.
type fileFingerprint struct {
	modUnixNano int64
	size        int64
}

// absFingerprint stats path and returns its absolute form (the stable cache
// key) plus its fingerprint. The absolute path matters for correctness: two
// different working directories can both hold a relative "x.go", and keying on
// the relative path would let one collide with the other in a long-lived
// process. Returns ok=false if the file cannot be stat'd (deleted/unreadable).
func absFingerprint(path string) (key string, fp fileFingerprint, ok bool) {
	fi, err := os.Stat(path)
	if err != nil {
		return "", fileFingerprint{}, false
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	return abs, fileFingerprint{modUnixNano: fi.ModTime().UnixNano(), size: fi.Size()}, true
}

// parsedFile is one cached parse result.
type parsedFile struct {
	fp  fileFingerprint
	ast *ast.File
}

// parseCache memoizes parsed ASTs across calls, sharing a single FileSet so that
// positions from any cached AST are resolvable. Re-parsing a changed file adds a
// fresh entry to the FileSet; the stale token.File is simply orphaned, so memory
// grows only with the number of edits, not the number of calls.
type parseCache struct {
	mu     sync.Mutex
	fset   *token.FileSet
	files  map[string]*parsedFile // abs path -> parsed
	parses int64                  // count of real ParseFile calls (cache misses), for tests
}

func newParseCache() *parseCache {
	return &parseCache{fset: token.NewFileSet(), files: map[string]*parsedFile{}}
}

// globalParseCache is the process-wide parse cache used by the live server.
var globalParseCache = newParseCache()

// load returns the shared FileSet and the ASTs for the requested files,
// re-parsing only files that are new or whose fingerprint changed. The returned
// map is keyed by the caller's original (possibly relative) paths so downstream
// code keeps reporting the same paths it always has. Files that cannot be read
// or parsed are skipped (best-effort, matching buildCallIndex and
// UseAnalyzer.Parse).
func (c *parseCache) load(files []string) (*token.FileSet, map[string]*ast.File) {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]*ast.File, len(files))
	for _, f := range files {
		key, fp, ok := absFingerprint(f)
		if !ok {
			continue
		}
		if pf, hit := c.files[key]; hit && pf.fp == fp {
			out[f] = pf.ast
			continue
		}
		content, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		// Parse with the original path so fset positions report it unchanged.
		node, err := parser.ParseFile(c.fset, f, content, parser.ParseComments)
		if err != nil {
			continue
		}
		c.parses++
		c.files[key] = &parsedFile{fp: fp, ast: node}
		out[f] = node
	}
	return c.fset, out
}

// parseCount reports how many real parses have happened over the cache's life.
// Tests use it to assert that warm calls hit the cache instead of re-parsing.
func (c *parseCache) parseCount() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.parses
}

// reset clears the cache and starts a fresh FileSet. Used by tests/benchmarks to
// measure cold-cache cost.
func (c *parseCache) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.fset = token.NewFileSet()
	c.files = map[string]*parsedFile{}
}
