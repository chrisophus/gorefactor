package astcache

import (
	"fmt"
	"go/ast"
	"go/token"
	"sort"
	"sync"

	"github.com/chrisophus/gorefactor/analyzer"
	"github.com/chrisophus/gorefactor/internal/cerr"
)

// CgDef is a function/method declaration found while indexing.
type CgDef struct {
	Name     string
	Receiver string
	File     string
	Line     int
}

// Key is the def's stable identifier ("Recv:Method" or "Func").
func (d *CgDef) Key() string {
	if d.Receiver != "" {
		return d.Receiver + ":" + d.Name
	}
	return d.Name
}

// CgIndex holds the bidirectional call-edge index for a file set.
type CgIndex struct {
	Defs    map[string]*CgDef   // key -> def
	Callees map[string][]*CgDef // caller key -> called defs
	Callers map[string][]*CgDef // callee key -> calling defs
}

// Lookup finds a definition by name and optional receiver. A bare name first
// matches a plain function, then falls back to a unique method of that name.
func (idx *CgIndex) Lookup(name, recv string) *CgDef {
	if recv != "" {
		return idx.Defs[recv+":"+name]
	}
	if d, ok := idx.Defs[name]; ok {
		return d
	}
	var found *CgDef
	for _, d := range idx.Defs {
		if d.Name == name {
			if found != nil {
				return nil // ambiguous: require Receiver:Method
			}
			found = d
		}
	}
	return found
}

// LookupTargetOrSuggest resolves a "Func" or "Receiver:Method" target, returning
// an exit-2 error listing candidate definitions when it is missing.
func (idx *CgIndex) LookupTargetOrSuggest(target string) (*CgDef, error) {
	name, recv := splitNameReceiver(target)
	def := idx.Lookup(name, recv)
	if def != nil {
		return def, nil
	}
	keys := make([]string, 0, len(idx.Defs))
	for k := range idx.Defs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) > 30 {
		keys = keys[:30]
	}
	return nil, cerr.NotFound(fmt.Sprintf("function %q not found", target), target, keys)
}

// BuildTree renders the call tree in the requested direction. visited holds the
// keys on the current root-to-node path, so revisits are marked [cycle] and not
// expanded further.
func (idx *CgIndex) BuildTree(def *CgDef, direction string, depth int, visited map[string]bool) *CgNode {
	node := &CgNode{Name: def.Name, Receiver: def.Receiver, File: def.File, Line: def.Line}
	if depth == 0 {
		return node
	}
	next := idx.Callees[def.Key()]
	if direction == "callers" {
		next = idx.Callers[def.Key()]
	}
	for _, child := range next {
		if visited[child.Key()] {
			node.Children = append(node.Children, &CgNode{
				Name: child.Name, Receiver: child.Receiver,
				File: child.File, Line: child.Line, Cycle: true,
			})
			continue
		}
		visited[child.Key()] = true
		node.Children = append(node.Children, idx.BuildTree(child, direction, depth-1, visited))
		delete(visited, child.Key())
	}
	return node
}

// TransitiveCallers returns every distinct definition that can reach def by
// following caller edges (def itself excluded). Each key is enqueued at most
// once, so cycles terminate.
func (idx *CgIndex) TransitiveCallers(def *CgDef) []*CgDef {
	seen := map[string]bool{def.Key(): true}
	var out []*CgDef
	queue := []*CgDef{def}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, caller := range idx.Callers[cur.Key()] {
			if seen[caller.Key()] {
				continue
			}
			seen[caller.Key()] = true
			out = append(out, caller)
			queue = append(queue, caller)
		}
	}
	return out
}

// CgNode is one function in a rendered call tree.
type CgNode struct {
	Name     string    `json:"name"`
	Receiver string    `json:"receiver,omitempty"`
	File     string    `json:"file"`
	Line     int       `json:"line"`
	Cycle    bool      `json:"cycle,omitempty"`
	Children []*CgNode `json:"children,omitempty"`
}

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
// calls until the file changes, and assembled into a fresh CgIndex each time.
type cgFileData struct {
	fp    fileFingerprint
	defs  []CgDef
	calls []cgRawCall
}

// CgReceiver returns the receiver type name of a method declaration.
func CgReceiver(fn *ast.FuncDecl) string {
	return analyzer.FuncReceiverName(fn)
}

// BuildCallIndex parses every file once and records call edges between declared
// functions. Selector calls (x.Foo()) are matched by method name; ident calls
// (Foo()) are matched against plain functions.
//
// It is backed by the process-global parse and call-index caches, so in a
// long-lived process — notably the `gorefactor mcp` server — unchanged files are
// neither re-read nor re-parsed nor re-walked across calls; only changed files
// (by mtime/size) are reprocessed and the cross-file edges are re-resolved. In
// one-shot CLI use the cache is populated once and discarded at exit, so
// behaviour and results are identical to a fresh build.
func BuildCallIndex(files []string) (*CgIndex, error) {
	return GlobalCallIndexCache.BuildWith(GlobalParseCache, files)
}

// CallIndexCache memoizes per-file cgFileData so unchanged files are never
// re-walked. The CgIndex itself is reassembled per call (edge resolution is
// cheap relative to parsing/walking), which keeps CgDef pointers valid for the
// index that owns them.
type CallIndexCache struct {
	mu       sync.Mutex
	files    map[string]cgFileData // abs path -> extracted data
	extracts int64                 // count of real per-file extractions (cache misses), for tests
}

// NewCallIndexCache returns an empty call-index cache.
func NewCallIndexCache() *CallIndexCache {
	return &CallIndexCache{files: map[string]cgFileData{}}
}

// BuildWith assembles a CgIndex for files, parsing/extracting only what changed.
// pc supplies (and caches) the ASTs; for unchanged files neither a read nor a
// parse nor an AST walk happens — only a stat.
func (c *CallIndexCache) BuildWith(pc *ParseCache, files []string) (*CgIndex, error) {
	fset, asts := pc.Load(files)

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

// ExtractCount reports how many real per-file extractions have happened over the
// cache's life. Tests use it to assert warm calls reuse cached data.
func (c *CallIndexCache) ExtractCount() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.extracts
}

// Reset clears the cache. Used by tests/benchmarks to measure cold-cache cost.
func (c *CallIndexCache) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.files = map[string]cgFileData{}
}

// GlobalCallIndexCache is the process-wide call-index cache used by the server.
var GlobalCallIndexCache = NewCallIndexCache()

// resetIndexCaches clears both process-global caches. Tests and benchmarks call
// it to force a cold rebuild; production code never needs it.
func resetIndexCaches() {
	GlobalParseCache.Reset()
	GlobalCallIndexCache.Reset()
}

// buildCallIndexUncached builds the index with fresh, throwaway caches, touching
// no global state. It always does a full cold parse+extract and exists for
// benchmarking the cache and as an independent oracle in correctness tests.
func buildCallIndexUncached(files []string) (*CgIndex, error) {
	return NewCallIndexCache().BuildWith(NewParseCache(), files)
}

// extractCgFileData walks one file's AST and records its declarations and call
// edges. This is the expensive per-file step BuildCallIndex used to redo on
// every call; caching it is the core Phase 2 win for callgraph/blast-radius.
func extractCgFileData(fset *token.FileSet, file string, astFile *ast.File, fp fileFingerprint) cgFileData {
	data := cgFileData{fp: fp}
	for _, decl := range astFile.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		def := CgDef{
			Name:     fn.Name.Name,
			Receiver: CgReceiver(fn),
			File:     file,
			Line:     fset.Position(fn.Pos()).Line,
		}
		data.defs = append(data.defs, def)
		callerKey := def.Key()
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

// assembleCallIndex builds a fresh CgIndex from per-file data. Definitions are
// inserted in file order with first-occurrence-wins (matching the original
// single-pass builder), then raw calls are resolved into bidirectional edges and
// each adjacency list is sorted for deterministic output.
func assembleCallIndex(files []string, perFile map[string]cgFileData) *CgIndex {
	idx := &CgIndex{
		Defs:    map[string]*CgDef{},
		Callees: map[string][]*CgDef{},
		Callers: map[string][]*CgDef{},
	}
	for _, file := range files {
		data, ok := perFile[file]
		if !ok {
			continue
		}
		for i := range data.defs {
			d := data.defs[i]
			if _, exists := idx.Defs[d.Key()]; !exists {
				dd := d
				idx.Defs[d.Key()] = &dd
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
			caller := idx.Defs[rc.callerKey]
			if caller == nil {
				continue
			}
			for _, callee := range resolveCallee(idx, rc.name, rc.selector) {
				edge := caller.Key() + "->" + callee.Key()
				if seen[edge] {
					continue
				}
				seen[edge] = true
				idx.Callees[caller.Key()] = append(idx.Callees[caller.Key()], callee)
				idx.Callers[callee.Key()] = append(idx.Callers[callee.Key()], caller)
			}
		}
	}
	for _, m := range []map[string][]*CgDef{idx.Callees, idx.Callers} {
		for k := range m {
			sort.Slice(m[k], func(i, j int) bool { return m[k][i].Key() < m[k][j].Key() })
		}
	}
	return idx
}

// resolveCallee maps a called name to candidate definitions. Without full type
// information, selector calls match methods of any receiver plus plain functions
// (package-qualified calls); ident calls match plain functions.
func resolveCallee(idx *CgIndex, name string, selector bool) []*CgDef {
	var out []*CgDef
	if d, ok := idx.Defs[name]; ok {
		out = append(out, d)
	}
	if selector {
		for k, d := range idx.Defs {
			if d.Name == name && containsColon(k) {
				out = append(out, d)
			}
		}
	}
	return out
}

func containsColon(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == ':' {
			return true
		}
	}
	return false
}

func splitNameReceiver(s string) (name, receiver string) {
	for i := 0; i < len(s); i++ {
		if s[i] == ':' {
			return s[i+1:], s[:i]
		}
	}
	return s, ""
}
