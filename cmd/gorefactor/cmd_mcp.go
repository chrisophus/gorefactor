package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chrisophus/gorefactor/version"
)

// gorefactor mcp — a stdio Model Context Protocol (MCP) server that exposes
// gorefactor's read-only analysis commands as native MCP tools. Any MCP client
// (Claude Code, Cursor, Copilot) can then call gorefactor's exact,
// AST/type-based Go intelligence — call graph, find-callers/uses,
// blast-radius, lint — through one endpoint.
//
// This is the Phase 0–1 slice of docs/mcp-server-plan.md. Per the plan's
// option to "adopt a Go MCP SDK", the transport is the official
// github.com/modelcontextprotocol/go-sdk, which gives us a spec-complete
// server (and a clean path to resources/prompts in Phase 4). Each tool wraps
// an existing Command: the server reconstructs an argv from the JSON
// tool-call arguments, runs Command.Run with stdout captured, and returns the
// captured text as the tool result. Mutation tools (Phase 3) and the
// long-lived index cache (Phase 2) are intentionally not part of this slice.

func init() {
	registerCommand(Command{
		Name:        "mcp",
		Description: "Run a stdio MCP server exposing gorefactor's read-only analysis tools",
		Usage:       "mcp",
		MinArgs:     0,
		MaxArgs:     0,
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
	server := newMCPServer(getCommands())
	return server.Run(context.Background(), &mcp.StdioTransport{})
}

// newMCPServer builds an SDK server with one tool per allowlisted, read-only
// command. The tools are registered via the low-level Server.AddTool so we can
// supply our own dynamically-generated input schema (one per command, derived
// from its flags) rather than a reflection-inferred Go struct schema.
func newMCPServer(cmds map[string]Command) *mcp.Server {
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

func toolDescription(cmd Command) string {
	desc := cmd.Description
	if cmd.Usage != "" {
		desc += "\n\nUsage: gorefactor " + cmd.Usage
	}
	return desc
}

// toolInputSchema derives a JSON Schema for a command's parameters: an `args`
// array for positional arguments (bounded by MinArgs/MaxArgs) plus one
// property per exposed flag (boolean flags -> boolean, value flags -> string).
// It is returned as json.RawMessage so the SDK passes it through verbatim
// (Server.AddTool does no schema inference or validation of its own).
func toolInputSchema(cmd Command) json.RawMessage {
	properties := map[string]interface{}{}
	var required []string

	if cmd.MaxArgs != 0 {
		argsSchema := map[string]interface{}{
			"type":        "array",
			"items":       map[string]interface{}{"type": "string"},
			"description": "Positional arguments, in order. See Usage in the tool description.",
		}
		if cmd.MinArgs > 0 {
			argsSchema["minItems"] = cmd.MinArgs
			required = append(required, "args")
		}
		if cmd.MaxArgs > 0 {
			argsSchema["maxItems"] = cmd.MaxArgs
		}
		properties["args"] = argsSchema
	}

	for _, flag := range sortedFlagNames(cmd.Flags) {
		if mcpSkipFlags[flag] {
			continue
		}
		key := flagParamName(flag)
		if cmd.Flags[flag] {
			properties[key] = map[string]interface{}{
				"type":        "string",
				"description": "Value for the " + flag + " flag.",
			}
		} else {
			properties[key] = map[string]interface{}{
				"type":        "boolean",
				"description": "Enable the " + flag + " flag.",
			}
		}
	}

	schema := map[string]interface{}{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	raw, err := json.Marshal(schema)
	if err != nil {
		// The schema is built from string keys and primitive values, so this
		// cannot fail in practice; fall back to a permissive object schema.
		return json.RawMessage(`{"type":"object"}`)
	}
	return raw
}

// flagParamName converts a CLI flag (e.g. "--in") into a JSON property name
// (e.g. "in"). JSON property names can't begin with a dash for many clients,
// and the leading dashes are noise in a structured schema.
func flagParamName(flag string) string {
	i := 0
	for i < len(flag) && flag[i] == '-' {
		i++
	}
	return flag[i:]
}

func sortedFlagNames(flags map[string]bool) []string {
	names := make([]string, 0, len(flags))
	for name := range flags {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// buildArgv turns the tool arguments object into the positional + flag argv
// the command expects, forcing --json when the command supports it so clients
// always receive structured output.
func buildArgv(cmd Command, raw json.RawMessage) ([]string, error) {
	args := map[string]json.RawMessage{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, fmt.Errorf("invalid arguments object: %w", err)
		}
	}

	var argv []string
	if v, ok := args["args"]; ok {
		var positional []string
		if err := json.Unmarshal(v, &positional); err != nil {
			return nil, fmt.Errorf("\"args\" must be an array of strings: %w", err)
		}
		argv = append(argv, positional...)
	}

	for _, flag := range sortedFlagNames(cmd.Flags) {
		if mcpSkipFlags[flag] {
			continue
		}
		key := flagParamName(flag)
		v, ok := args[key]
		if !ok {
			continue
		}
		if cmd.Flags[flag] {
			var val string
			if err := json.Unmarshal(v, &val); err != nil {
				return nil, fmt.Errorf("%q must be a string", key)
			}
			argv = append(argv, flag, val)
		} else {
			var on bool
			if err := json.Unmarshal(v, &on); err != nil {
				return nil, fmt.Errorf("%q must be a boolean", key)
			}
			if on {
				argv = append(argv, flag)
			}
		}
	}

	if _, supportsJSON := cmd.Flags["--json"]; supportsJSON {
		argv = append(argv, "--json")
	}

	if err := checkCommandArgs(cmd, argv); err != nil {
		return nil, err
	}
	return argv, nil
}

func toolTextResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}
}

func toolErrorResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
		IsError: true,
	}
}
