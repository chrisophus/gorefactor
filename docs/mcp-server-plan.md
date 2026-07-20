# Plan: `gorefactor mcp` — an MCP server for Go code intelligence

A plan for exposing gorefactor over the **Model Context Protocol**, so any MCP
client (Claude Code, Cursor, Copilot) gets gorefactor's analysis *and* its safe
structural edits as native tools — the way [codeindexer.dev](https://codeindexer.dev/)
exposes its 32 tools over MCP.

**Status:** Phases 0–4 are implemented (`gorefactor mcp`). Read-only analysis
tools ship by default; the mutation guides are exposed behind `--allow-write`
(Phase 3), and `skeleton`/`inspect`/`context` are served as MCP resources with
an `init-agent-rules --mcp` installer (Phase 4). Phase 2 (the long-lived index
cache) is now implemented in `cmd_callgraph_build.go` + `index_cache.go` /
`index_cache_callgraph.go`: a process-global parse cache (shared by the call
graph and the `find-*` analyzers) plus a per-file call-index cache, both keyed
by file mtime+size. Benchmarks in `index_cache_test.go` show ~2.8× faster
call-index rebuilds and ~4× faster `find-callers` on a warm cache, with
16–18× fewer allocations. Prompts (part of Phase 4) remain the one open item.

## Why gorefactor is well-positioned

codeindexer is read-only, language-agnostic, and embedding/best-effort based.
gorefactor is the opposite where it counts for Go:

- **AST/type-exact for Go** — no semantic-search guessing for a Go-only repo.
- **It mutates safely** — the guides (`create`/`insert`/`replace`/`move`/…)
  refuse malformed Go. An MCP gorefactor server can give an agent *both*
  precise analysis and deterministic edits through one endpoint. codeindexer
  can't edit at all.

So the pitch is not "reimplement codeindexer" but "expose the harness we
already have over the same protocol."

## The key insight: the command registry already is a tool catalog

Every command self-registers a `Command{Name, Description, Usage, MinArgs,
MaxArgs, Flags, Run}` into `commandRegistry` (`cmd/gorefactor/registry.go`),
reachable via `getCommands()`. That struct is almost an MCP tool definition
already:

| MCP tool field | gorefactor source |
|----------------|-------------------|
| `name` | `Command.Name` |
| `description` | `Command.Description` (+ `Usage`) |
| `inputSchema` | derived from `MinArgs`/`MaxArgs` (positional args) + `Flags` (booleans/strings) |
| handler | wrap `Command.Run` |

`Run` writes to stdout; `captureStdoutOf(fn)` (`cmd/gorefactor/cmd_txn.go:225`)
already captures that into a string for the tool result. So the MCP layer is a
thin adapter, not a rewrite — most of the work is transport + schema generation.

## Phased plan

### Phase 0 — transport decision
MCP is JSON-RPC 2.0, typically over **stdio** for local servers. Two options:

- **Minimal in-house stdio JSON-RPC** (~a few hundred lines): `initialize`,
  `tools/list`, `tools/call`. Keeps gorefactor's deliberately dependency-light
  posture (it already dropped external `goimports` for an in-process version).
- **Adopt a Go MCP SDK** (e.g. `modelcontextprotocol/go-sdk` or
  `mark3labs/mcp-go`): faster, spec-complete (resources, prompts, progress),
  but a real dependency.

Recommendation: start with the **minimal in-house** server for `tools/*` to ship
an MVP without new deps; revisit an SDK if/when we want resources & prompts
(Phase 4).

### Phase 1 — read-only analysis tools (MVP)
New command `gorefactor mcp` (stdio server). Auto-generate one tool per
**read-only** registered command, preferring JSON output:
`parse`, `skeleton`, `inspect`, `context`, `callgraph`, **`blast-radius`**,
`find-callers`, `find-uses`, `find-implementations`, `search-ast`, `recommend`,
`review`, `api-diff`, `test-affected`, `lint`.

- Build args from the JSON tool-call params using each command's `Flags`/arg
  bounds; invoke `Run` via `captureStdoutOf`; return stdout as the tool result.
- Force `--json` where the command supports it so clients get structured data.
- Derive the read-only tool allowlist from per-command I/O metadata
  (`MCPTool && ReadOnly` on `Command`, exposed via `mcpReadOnlyTools()`); a
  command opts in by setting `MCPTool` at registration rather than being added
  to a hand-synced slice, so mutators can never leak into the read-only surface.

### Phase 2 — long-lived index cache (the "index once" win) ✅ implemented
codeindexer's speed comes from indexing once into a graph DB. gorefactor
rebuilds the call index (`buildCallIndex`) per invocation — fine for one-shot
CLI, wasteful for a long-lived server. Implemented as two process-global,
mtime+size-keyed caches:

- **`parseCache`** (`index_cache.go`): path → parsed `*ast.File` over a shared
  `FileSet`. Shared by the call-graph builder *and* the `find-*` analyzers via
  `UseAnalyzer.SeedASTs`, so a `find-callers` call reuses ASTs a prior
  `callgraph` call already parsed.
- **`callIndexCache`** (`index_cache_callgraph.go`): path → per-file defs + raw
  call edges, so `buildCallIndex` skips the AST walk for unchanged files and
  only re-resolves the (cheap) cross-file edges.
- The file *set* is intentionally not cached: callers re-walk the tree each call
  (`collectGoFiles`), so added/removed files are always seen; only the expensive
  per-file parse/extract step is memoized. Invalidation is by file mtime+size
  (the build-cache/gopls signal), no fsnotify dependency.
- `callgraph`/`blast-radius`/the `high-blast-radius` lint rule go through
  `buildCallIndex` and benefit transparently; `find-callers`/`find-uses`/
  `find-implementations` seed their analyzer from the shared parse cache.
- Validated by benchmarks in `index_cache_test.go` (warm vs cold): ~2.8× faster
  call-index rebuilds, ~4× faster `find-callers`, 16–18× fewer allocations.

### Phase 3 — opt-in mutation tools (the differentiator) ✅ implemented
Behind a `--allow-write` flag (default **off**), expose the guides:
`create`, `insert`, `replace`, `replace-text`, `replace-body`, `move`,
`rename`, `delete`, `add-field`, `change-signature`, etc., plus `txn` for
all-or-nothing batches.

Safety model, reusing what exists:
- Require a clean git worktree (mirror `requireCleanWorktree` in the agent loop)
  so every edit is reversible via `git reset --hard`, and surface `undo`
  (`.gorefactor/` snapshots) as a tool.
- Annotate these tools as destructive in their MCP descriptions so clients
  prompt for approval.

Implemented in `cmd/gorefactor/cmd_mcp_write.go`: the write-tool allowlist is
derived from per-command I/O metadata (`MCPTool && Mutates`, via
`mcpWriteTools()`, mirroring `mcpReadOnlyTools()`), each tool annotated
`DestructiveHint=true` with a "Modifies Go source on disk." description prefix
and an `IdempotentHint` taken from the command's `Idempotent` metadata, plus
`undo` for snapshot rollback. `mcpRequireCleanWorktree` enforces the clean
baseline at startup unless `--allow-dirty` is passed.

### Phase 4 — resources, prompts, installer ✅ (resources + installer implemented)
- **MCP resources**: serve `skeleton`/`context` packs and `inspect` summaries as
  readable resources (token-cheap context the client can pull on demand).
  Implemented in `cmd_mcp_resources.go` as three URI templates:
  `gorefactor://skeleton/{+path}`, `gorefactor://inspect/{+path}`,
  `gorefactor://context/{symbol}`.
- **Installer**: `init-agent-rules --mcp` (or `--mcp-only`) merges a
  `gorefactor` entry into `.mcp.json` with `command: gorefactor, args: ["mcp"]`,
  preserving any existing servers and never clobbering a customised entry.
- **Prompts**: not yet implemented.

### Cross-cutting: testing & security
- **Testing**: table-driven tests over the JSON-RPC envelope (`initialize`,
  `tools/list`, a `tools/call` per command) using the same temp-fixture style as
  `cmd_callgraph_test.go`; assert generated `inputSchema` matches each command's
  `Flags`.
- **Security**: read-only by default; `--allow-write` gated and clean-worktree
  enforced; scope file access to the target module root; never shell out beyond
  the existing command set.

## Smallest shippable slice
`gorefactor mcp` (stdio) + `tools/list`/`tools/call` over the read-only
allowlist, returning `--json` output (Phases 0–1). That alone gives an agent a
precise Go call graph, semantic-free exact `find-callers`/`find-uses`, and the
new `blast-radius` score — the codeindexer feature set, but exact for Go — in
roughly a single new file plus a schema-generation helper.
