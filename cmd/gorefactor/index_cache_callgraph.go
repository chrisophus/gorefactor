package main

import (
	"go/ast"
	"go/token"
	"sort"
	"sync"
)

// cgRawCall is one unresolved call edge: the caller's index key plus the called
// name. selector marks x.Foo()-style calls (method or package-qualified) so the
// resolver can match them against methods of any receiver.
type cgRawCall struct {
	callerKey string
	name      string
	selector  bool
}

// cgFileData is the query-independent call information extracted from one file:
// the functions/methods it declares and the raw call edges in their bodies.
// Because it is keyed only by fingerprint, it can be reused verbatim across
// calls until the file changes, and assembled into a fresh cgIndex each time.
type cgFileData struct {
	fp    fileFingerprint
	defs  []cgDef
	calls []cgRawCall
}

// extractCgFileData walks one file's AST and records its declarations and call
// edges. This is the expensive per-file step buildCallIndex used to redo on
// every call; caching it is the core Phase 2 win for callgraph/blast-radius.
func extractCgFileData(fset *token.FileSet, file string, astFile *ast.File, fp fileFingerprint) cgFileData {
	data := cgFileData{fp: fp}
	for _, decl := range astFile.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		def := cgDef{
			name:     fn.Name.Name,
			receiver: cgReceiver(fn),
			file:     file,
			line:     fset.Position(fn.Pos()).Line,
		}
		data.defs = append(data.defs, def)
		callerKey := def.key()
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch f := call.Fun.(type) {
			case *ast.Ident:
				data.calls = append(data.calls, cgRawCall{callerKey, f.Name, false})
			case *ast.SelectorExpr:
				data.calls = append(data.calls, cgRawCall{callerKey, f.Sel.Name, true})
			}
			return true
		})
	}
	return data
}

// callIndexCache memoizes per-file cgFileData so unchanged files are never
// re-walked. The cgIndex itself is reassembled per call (edge resolution is
// cheap relative to parsing/walking), which keeps cgDef pointers valid for the
// index that owns them.
type callIndexCache struct {
	mu       sync.Mutex
	files    map[string]cgFileData // abs path -> extracted data
	extracts int64                 // count of real per-file extractions (cache misses), for tests
}

func newCallIndexCache() *callIndexCache {
	return &callIndexCache{files: map[string]cgFileData{}}
}

// globalCallIndexCache is the process-wide call-index cache used by the server.
var globalCallIndexCache = newCallIndexCache()

// buildWith assembles a cgIndex for files, parsing/extracting only what changed.
// pc supplies (and caches) the ASTs; for unchanged files neither a read nor a
// parse nor an AST walk happens — only a stat.
func (c *callIndexCache) buildWith(pc *parseCache, files []string) (*cgIndex, error) {
	fset, asts := pc.load(files)

	c.mu.Lock()
	perFile := make(map[string]cgFileData, len(files))
	for _, file := range files {
		key, fp, ok := absFingerprint(file)
		if !ok {
			continue
		}
		if cached, hit := c.files[key]; hit && cached.fp == fp {
			perFile[file] = cached
			continue
		}
		astFile, ok := asts[file]
		if !ok {
			continue // unreadable/unparseable: best-effort skip
		}
		data := extractCgFileData(fset, file, astFile, fp)
		c.extracts++
		c.files[key] = data
		perFile[file] = data
	}
	c.mu.Unlock()

	return assembleCallIndex(files, perFile), nil
}

// assembleCallIndex builds a fresh cgIndex from per-file data. Definitions are
// inserted in file order with first-occurrence-wins (matching the original
// single-pass builder), then raw calls are resolved into bidirectional edges and
// each adjacency list is sorted for deterministic output.
func assembleCallIndex(files []string, perFile map[string]cgFileData) *cgIndex {
	idx := &cgIndex{
		defs:    map[string]*cgDef{},
		callees: map[string][]*cgDef{},
		callers: map[string][]*cgDef{},
	}
	for _, file := range files {
		data, ok := perFile[file]
		if !ok {
			continue
		}
		for i := range data.defs {
			d := data.defs[i]
			if _, exists := idx.defs[d.key()]; !exists {
				dd := d
				idx.defs[d.key()] = &dd
			}
		}
	}
	seen := map[string]bool{} // "callerKey->calleeKey" dedupe
	for _, file := range files {
		data, ok := perFile[file]
		if !ok {
			continue
		}
		for _, rc := range data.calls {
			caller := idx.defs[rc.callerKey]
			if caller == nil {
				continue
			}
			for _, callee := range resolveCallee(idx, rc.name, rc.selector) {
				edge := caller.key() + "->" + callee.key()
				if seen[edge] {
					continue
				}
				seen[edge] = true
				idx.callees[caller.key()] = append(idx.callees[caller.key()], callee)
				idx.callers[callee.key()] = append(idx.callers[callee.key()], caller)
			}
		}
	}
	for _, m := range []map[string][]*cgDef{idx.callees, idx.callers} {
		for k := range m {
			sort.Slice(m[k], func(i, j int) bool { return m[k][i].key() < m[k][j].key() })
		}
	}
	return idx
}

// extractCount reports how many real per-file extractions have happened over
// the cache's life. Tests use it to assert warm calls reuse cached data.
func (c *callIndexCache) extractCount() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.extracts
}

// reset clears the cache. Used by tests/benchmarks to measure cold-cache cost.
func (c *callIndexCache) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.files = map[string]cgFileData{}
}

// resetIndexCaches clears both process-global caches. Tests and benchmarks call
// it to force a cold rebuild; production code never needs it.
func resetIndexCaches() {
	globalParseCache.reset()
	globalCallIndexCache.reset()
}
