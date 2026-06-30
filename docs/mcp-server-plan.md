# Plan: `gorefactor mcp` — an MCP server for Go code intelligence

A plan for exposing gorefactor over the **Model Context Protocol**, so any MCP
client (Claude Code, Cursor, Copilot) gets gorefactor's analysis *and* its safe
structural edits as native tools — the way [codeindexer.dev](https://codeindexer.dev/)
exposes its 32 tools over MCP.

This is a design plan, not an implemented feature.

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
- Maintain an explicit allowlist of read-only command names (don't expose
  mutators yet).

### Phase 2 — long-lived index cache (the "index once" win)
codeindexer's speed comes from indexing once into a graph DB. gorefactor
rebuilds the call index (`buildCallIndex`) per invocation — fine for one-shot
CLI, wasteful for a long-lived server. Add an in-process cache:

- Parse the package set / call index once; key entries by file mtime.
- Invalidate only changed files on each tool call (or watch with fsnotify).
- This is where `callgraph`/`blast-radius`/`find-*` get their biggest speedup in
  server mode.

### Phase 3 — opt-in mutation tools (the differentiator)
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

### Phase 4 — resources, prompts, installer
- **MCP resources**: serve `skeleton`/`context` packs and `inspect` summaries as
  readable resources (token-cheap context the client can pull on demand).
- **Installer**: extend the existing `init-agent-rules` command to also emit the
  MCP client config snippet (`.mcp.json` / `claude_desktop_config.json`) with
  `command: gorefactor, args: ["mcp"]`.

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
