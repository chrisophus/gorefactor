package main

// The by-name call-graph index and its long-lived caches now live in
// internal/astcache so the MCP server and other importers can build a call graph
// without depending on package main. These aliases preserve the historical
// spellings used across the callgraph, blast-radius, and context commands.

import "github.com/chrisophus/gorefactor/internal/astcache"

type (
	cgIndex = astcache.CgIndex
	cgDef   = astcache.CgDef
	cgNode  = astcache.CgNode
)

var (
	buildCallIndex = astcache.BuildCallIndex
	cgReceiver     = astcache.CgReceiver
)
