package main

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Phase 3 of docs/mcp-server-plan.md: opt-in mutation tools. When the server
// is started with --allow-write, the safe-edit *guides*
// (create/insert/replace/move/rename/delete/...) are exposed as MCP tools in
// addition to the read-only analysis tools. They are the differentiator over a
// read-only indexer: an agent gets precise Go analysis *and* deterministic,
// AST-validated edits through one endpoint, and because each guide parses
// before it writes, the failure mode is "tool rejects the change" rather than
// "malformed Go on disk".
//
// Safety model (mirrors the gorefactor-agent loop):
//   - --allow-write is OFF by default; a default server is read-only.
//   - Unless --allow-dirty is passed, the worktree must be clean at startup so
//     every edit is reversible with `git reset --hard` back to that baseline.
//   - `undo` (the .gorefactor/ snapshot rollback) is surfaced as a tool.
//   - Each tool is annotated destructive so clients prompt for approval.

// mcpWriteTools is the allowlist of registered mutation commands exposed as MCP
// tools when --allow-write is set. It is derived from the per-command I/O
// metadata (MCPTool && Mutates) rather than hand-maintained, so a mutation
// command is exposed for writing only by deliberately setting MCPTool at
// registration — it can never drift out of sync with a parallel slice, nor be
// auto-exposed without the flag.
func mcpWriteTools() []string {
	return commandsWhere(func(c Command) bool { return c.MCPTool && c.Mutates })
}

// registerMCPWriteTools adds the mutation guides to the server. It reuses the
// same argv-reconstruction handler as the read-only tools; only the tool
// annotations differ (destructive, not read-only). The IdempotentHint comes
// from the command's own Idempotent metadata (e.g. format reaches a gofmt fixed
// point, a repeated undo of the same entry is a no-op).
func registerMCPWriteTools(server *mcp.Server, cmds map[string]Command) {
	destructive := true
	for _, name := range mcpWriteTools() {
		cmd, ok := cmds[name]
		if !ok {
			continue
		}
		idempotent := cmd.Idempotent
		tool := &mcp.Tool{
			Name:        name,
			Description: writeToolDescription(cmd),
			InputSchema: toolInputSchema(cmd),
			Annotations: &mcp.ToolAnnotations{
				ReadOnlyHint:    false,
				DestructiveHint: &destructive,
				IdempotentHint:  idempotent,
			},
		}
		server.AddTool(tool, toolHandler(cmd))
	}
}

// writeToolDescription prefixes the command's own description with a clear
// "modifies files" note so a client (and the human approving the call) sees
// the mutation up front, independent of whether it renders tool annotations.
func writeToolDescription(cmd Command) string {
	return "Modifies Go source on disk. " + toolDescription(cmd)
}

// mcpRequireCleanWorktree enforces the same precondition as the agent loop's
// requireCleanWorktree: the target must be a git work tree with no
// uncommitted changes, so the server's edits are fully reversible. It is a
// standalone copy because the agent's version lives in a different package
// (cmd/gorefactor-agent) and the MCP server must not depend on it.
func mcpRequireCleanWorktree(dir string) error {
	if out, err := exec.Command("git", "-C", dir, "rev-parse", "--is-inside-work-tree").CombinedOutput(); err != nil {
		return fmt.Errorf("mcp --allow-write requires a git work tree for safe rollback: %s",
			strings.TrimSpace(string(out)))
	}
	out, err := exec.Command("git", "-C", dir, "status", "--porcelain").CombinedOutput()
	if err != nil {
		return fmt.Errorf("git status: %w", err)
	}
	if strings.TrimSpace(string(out)) != "" {
		return fmt.Errorf("working tree is dirty; commit or stash first so edits are reversible, or pass --allow-dirty")
	}
	return nil
}
