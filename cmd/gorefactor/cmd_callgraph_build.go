package main

// buildCallIndex parses every file once and records call edges between declared
// functions. Selector calls (x.Foo()) are matched by method name; ident calls
// (Foo()) are matched against plain functions.
//
// It is backed by the process-global parse and call-index caches (see
// index_cache.go), so in a long-lived process — notably the `gorefactor mcp`
// server — unchanged files are neither re-read nor re-parsed nor re-walked
// across calls; only changed files (by mtime/size) are reprocessed and the
// cross-file edges are re-resolved. In one-shot CLI use the cache is populated
// once and discarded at exit, so behaviour and results are identical to a fresh
// build.
func buildCallIndex(files []string) (*cgIndex, error) {
	return globalCallIndexCache.buildWith(globalParseCache, files)
}

// buildCallIndexUncached builds the index with fresh, throwaway caches, touching
// no global state. It always does a full cold parse+extract and exists for
// benchmarking the cache and as an independent oracle in correctness tests.
func buildCallIndexUncached(files []string) (*cgIndex, error) {
	return newCallIndexCache().buildWith(newParseCache(), files)
}
