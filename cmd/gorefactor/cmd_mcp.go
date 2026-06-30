package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/chrisophus/gorefactor/version"
)

// gorefactor mcp — a minimal stdio Model Context Protocol (MCP) server that
// exposes gorefactor's read-only analysis commands as native MCP tools. Any
// MCP client (Claude Code, Cursor, Copilot) can then call gorefactor's exact,
// AST/type-based Go intelligence — call graph, find-callers/uses, blast-radius,
// lint — through one endpoint.
//
// This is the Phase 0–1 slice of docs/mcp-server-plan.md: an in-house
// newline-delimited JSON-RPC 2.0 server (no new dependencies) serving
// initialize / tools/list / tools/call over an allowlist of read-only
// commands. Each tool wraps an existing Command: the server reconstructs an
// argv from the JSON tool-call arguments, runs Command.Run with stdout
// captured, and returns the captured text as the tool result. Mutation tools
// (Phase 3) and the long-lived index cache (Phase 2) are intentionally not
// part of this slice.

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
	srv := newMCPServer(getCommands())
	return srv.serve(os.Stdin, os.Stdout)
}

// mcpServer holds the resolved set of exposed tools.
type mcpServer struct {
	tools map[string]Command // tool name -> backing command
	names []string           // sorted exposed tool names
}

func newMCPServer(cmds map[string]Command) *mcpServer {
	s := &mcpServer{tools: map[string]Command{}}
	for _, name := range mcpReadOnlyTools {
		if cmd, ok := cmds[name]; ok {
			s.tools[name] = cmd
			s.names = append(s.names, name)
		}
	}
	sort.Strings(s.names)
	return s
}

// --- JSON-RPC 2.0 envelope types -------------------------------------------

type jsonrpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonrpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *jsonrpcError   `json:"error,omitempty"`
}

type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

const (
	rpcMethodNotFound = -32601
	rpcInvalidParams  = -32602
)

// serve runs the stdio message loop. MCP's stdio transport frames each
// JSON-RPC message as a single newline-delimited line, so we scan line by
// line. Requests (with an id) get a response; notifications (no id) do not.
func (s *mcpServer) serve(in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var req jsonrpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			// We cannot recover an id from an unparseable line; skip it.
			continue
		}
		resp, respond := s.dispatch(&req)
		if !respond {
			continue
		}
		if err := writeJSONLine(out, resp); err != nil {
			return err
		}
	}
	return scanner.Err()
}

// dispatch routes one request. The second return reports whether a response
// should be written (notifications return false).
func (s *mcpServer) dispatch(req *jsonrpcRequest) (jsonrpcResponse, bool) {
	isNotification := len(req.ID) == 0
	resp := jsonrpcResponse{JSONRPC: "2.0", ID: req.ID}

	switch req.Method {
	case "initialize":
		resp.Result = s.handleInitialize(req.Params)
	case "notifications/initialized", "notifications/cancelled", "ping":
		if req.Method == "ping" {
			resp.Result = map[string]interface{}{}
			return resp, !isNotification
		}
		// Acknowledged notifications carry no id and need no reply.
		return resp, false
	case "tools/list":
		resp.Result = map[string]interface{}{"tools": s.toolDefinitions()}
	case "tools/call":
		result, rpcErr := s.handleToolsCall(req.Params)
		if rpcErr != nil {
			resp.Error = rpcErr
		} else {
			resp.Result = result
		}
	default:
		resp.Error = &jsonrpcError{Code: rpcMethodNotFound, Message: "method not found: " + req.Method}
	}

	if isNotification {
		return resp, false
	}
	return resp, true
}

func (s *mcpServer) handleInitialize(params json.RawMessage) map[string]interface{} {
	protocolVersion := "2024-11-05"
	if len(params) > 0 {
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if err := json.Unmarshal(params, &p); err == nil && p.ProtocolVersion != "" {
			protocolVersion = p.ProtocolVersion
		}
	}
	return map[string]interface{}{
		"protocolVersion": protocolVersion,
		"capabilities": map[string]interface{}{
			"tools": map[string]interface{}{},
		},
		"serverInfo": map[string]interface{}{
			"name":    "gorefactor",
			"version": version.Version,
		},
	}
}

// mcpTool is the MCP tool descriptor returned by tools/list.
type mcpTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

func (s *mcpServer) toolDefinitions() []mcpTool {
	tools := make([]mcpTool, 0, len(s.names))
	for _, name := range s.names {
		cmd := s.tools[name]
		tools = append(tools, mcpTool{
			Name:        name,
			Description: toolDescription(cmd),
			InputSchema: toolInputSchema(cmd),
		})
	}
	return tools
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
func toolInputSchema(cmd Command) map[string]interface{} {
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
	return schema
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

// handleToolsCall reconstructs an argv from the JSON arguments and runs the
// backing command with stdout captured. A command that returns an error
// becomes an MCP tool error (isError=true) carrying the message, so the model
// sees the failure rather than the JSON-RPC layer swallowing it.
func (s *mcpServer) handleToolsCall(params json.RawMessage) (map[string]interface{}, *jsonrpcError) {
	var call struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &call); err != nil {
		return nil, &jsonrpcError{Code: rpcInvalidParams, Message: "invalid params: " + err.Error()}
	}
	cmd, ok := s.tools[call.Name]
	if !ok {
		return nil, &jsonrpcError{Code: rpcInvalidParams, Message: "unknown tool: " + call.Name}
	}

	argv, err := buildArgv(cmd, call.Arguments)
	if err != nil {
		return nil, &jsonrpcError{Code: rpcInvalidParams, Message: err.Error()}
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
		return toolTextResult(text, true), nil
	}
	return toolTextResult(output, false), nil
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

func toolTextResult(text string, isError bool) map[string]interface{} {
	return map[string]interface{}{
		"content": []map[string]interface{}{
			{"type": "text", "text": text},
		},
		"isError": isError,
	}
}

func writeJSONLine(out io.Writer, v interface{}) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = out.Write(b)
	return err
}
