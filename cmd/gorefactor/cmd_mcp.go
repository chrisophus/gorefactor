package main

import (
	"context"
	"sort"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chrisophus/gorefactor/version"
)

// gorefactor mcp — a stdio Model Context Protocol (MCP) server that exposes
// gorefactor's Go code intelligence as native MCP tools. Any MCP client
// (Claude Code, Cursor, Copilot) can then call gorefactor's exact,
// AST/type-based Go intelligence — call graph, find-callers/uses,
// blast-radius, lint — through one endpoint.
//
// Per docs/mcp-server-plan.md's option to "adopt a Go MCP SDK", the transport
// is the official github.com/modelcontextprotocol/go-sdk, which gives us a
// spec-complete server. Each tool wraps an existing Command: the server
// reconstructs an argv from the JSON tool-call arguments, runs Command.Run
// with stdout captured, and returns the captured text as the tool result.
//
// Coverage: read-only analysis tools (Phase 1, this file), opt-in mutation
// guides behind --allow-write (Phase 3, cmd_mcp_write.go), and
// skeleton/inspect/context resources (Phase 4, cmd_mcp_resources.go). The
// argv/schema helpers live in cmd_mcp_schema.go. The long-lived index cache
// (Phase 2) is the remaining open item.

func init() {
	registerCommand(Command{
		Name:        "mcp",
		Description: "Run a stdio MCP server exposing gorefactor's Go code intelligence as MCP tools (read-only by default; --allow-write enables the mutation guides)",
		Usage:       "mcp [--allow-write] [--allow-dirty]",
		MinArgs:     0,
		MaxArgs:     0,
		Flags:       map[string]bool{"--allow-write": false, "--allow-dirty": false},
		Run:         mcpCommand,
	})
}

// mcpReadOnlyTools is the allowlist of registered commands exposed as MCP
// tools. Only read-only analysis commands appear here; mutators are withheld
// until the Phase 3 --allow-write work. Keeping this explicit (rather than
// "everything not a mutator") makes it impossible to leak a destructive
// command into the server by accident.
var mcpReadOnlyTools = []string{
	"parse",
	"list-functions",
	"skeleton",
	"inspect",
	"context",
	"callgraph",
	"blast-radius",
	"find-callers",
	"find-uses",
	"find-implementations",
	"find-package-deps",
	"search-ast",
	"recommend",
	"suggest-plan",
	"review",
	"api-diff",
	"test-affected",
	"lint",
}

// mcpSkipFlags are flags that must never be surfaced as tool parameters.
// --json is forced on by the server (clients always get structured output),
// --fix mutates files, and the rest are internal/diagnostic toggles.
var mcpSkipFlags = map[string]bool{
	"--help":          true,
	"--json":          true,
	"--fix":           true,
	"--quiet":         true,
	"--cpuprofile":    true,
	"--profile-rules": true,
	"-stdin":          true,
	"-o":              true,
}

func mcpCommand(args []string) error {
	_, flags := parseFlags(args, map[string]bool{"--allow-write": false, "--allow-dirty": false})
	allowWrite := flags["--allow-write"] != ""
	allowDirty := flags["--allow-dirty"] != ""

	if allowWrite && !allowDirty {
		// Mirror the agent loop's safety model: a clean worktree at startup
		// means every edit the server makes is reversible with a single
		// `git reset --hard` (plus `git clean -fd`) back to this baseline.
		if err := mcpRequireCleanWorktree("."); err != nil {
			return err
		}
	}

	server := newMCPServer(getCommands(), allowWrite)
	return server.Run(context.Background(), &mcp.StdioTransport{})
}

// newMCPServer builds an SDK server with one tool per allowlisted command. The
// tools are registered via the low-level Server.AddTool so we can supply our
// own dynamically-generated input schema (one per command, derived from its
// flags) rather than a reflection-inferred Go struct schema. Read-only
// analysis tools are always registered; the mutation guides and the
// skeleton/inspect/context resources are added only when allowWrite is set.
func newMCPServer(cmds map[string]Command, allowWrite bool) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "gorefactor",
		Version: version.Version,
	}, nil)

	names := append([]string(nil), mcpReadOnlyTools...)
	sort.Strings(names)
	for _, name := range names {
		cmd, ok := cmds[name]
		if !ok {
			continue
		}
		tool := &mcp.Tool{
			Name:        name,
			Description: toolDescription(cmd),
			InputSchema: toolInputSchema(cmd),
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
		}
		server.AddTool(tool, toolHandler(cmd))
	}

	registerMCPResources(server, cmds)

	if allowWrite {
		registerMCPWriteTools(server, cmds)
	}
	return server
}

// toolHandler adapts a Command into an MCP ToolHandler. It reconstructs an
// argv from the raw JSON arguments and runs the command with stdout captured.
// A command that returns an error becomes an MCP tool error (IsError) carrying
// the message, so the model sees the failure and can self-correct, rather than
// the failure surfacing as a protocol-level error.
func toolHandler(cmd Command) mcp.ToolHandler {
	return func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		argv, err := buildArgv(cmd, req.Params.Arguments)
		if err != nil {
			return toolErrorResult(err.Error()), nil
		}
		var runErr error
		output := captureStdoutOf(func() {
			runErr = cmd.Run(argv)
		})
		if runErr != nil {
			text := runErr.Error()
			if output != "" {
				text = output + "\n" + text
			}
			return toolErrorResult(text), nil
		}
		return toolTextResult(output), nil
	}
}
