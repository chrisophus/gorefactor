package main

import (
	"encoding/json"
	"strings"
	"testing"
)

const mcpTestSrc = `package x

func Top() {
	Middle()
}

func Middle() {
	Leaf()
}

func Leaf() {}
`

// roundTrip feeds the server one JSON-RPC request line and returns the decoded
// response. It exercises the real serve() loop over an in-memory pipe.
func roundTrip(t *testing.T, srv *mcpServer, req map[string]interface{}) jsonrpcResponse {
	t.Helper()
	line, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	var out strings.Builder
	if err := srv.serve(strings.NewReader(string(line)+"\n"), &out); err != nil {
		t.Fatalf("serve: %v", err)
	}
	var resp jsonrpcResponse
	trimmed := strings.TrimSpace(out.String())
	if trimmed == "" {
		t.Fatalf("no response for request %v", req)
	}
	if err := json.Unmarshal([]byte(trimmed), &resp); err != nil {
		t.Fatalf("unmarshal response %q: %v", trimmed, err)
	}
	return resp
}

func newTestServer() *mcpServer {
	return newMCPServer(getCommands())
}

func TestMCPInitialize(t *testing.T) {
	srv := newTestServer()
	resp := roundTrip(t, srv, map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params":  map[string]interface{}{"protocolVersion": "2025-06-18"},
	})
	if resp.Error != nil {
		t.Fatalf("initialize error: %+v", resp.Error)
	}
	result, _ := resp.Result.(map[string]interface{})
	if result["protocolVersion"] != "2025-06-18" {
		t.Errorf("protocolVersion = %v, want echoed 2025-06-18", result["protocolVersion"])
	}
	info, _ := result["serverInfo"].(map[string]interface{})
	if info["name"] != "gorefactor" {
		t.Errorf("serverInfo.name = %v, want gorefactor", info["name"])
	}
}

func TestMCPNotificationNoResponse(t *testing.T) {
	srv := newTestServer()
	var out strings.Builder
	// A notification (no id) must produce no response line.
	if err := srv.serve(strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}`+"\n"), &out); err != nil {
		t.Fatalf("serve: %v", err)
	}
	if strings.TrimSpace(out.String()) != "" {
		t.Errorf("notification produced a response: %q", out.String())
	}
}

func TestMCPToolsListSchema(t *testing.T) {
	srv := newTestServer()
	resp := roundTrip(t, srv, map[string]interface{}{
		"jsonrpc": "2.0", "id": 2, "method": "tools/list",
	})
	if resp.Error != nil {
		t.Fatalf("tools/list error: %+v", resp.Error)
	}
	result, _ := resp.Result.(map[string]interface{})
	rawTools, _ := result["tools"].([]interface{})
	if len(rawTools) != len(srv.names) {
		t.Fatalf("tools/list returned %d tools, want %d", len(rawTools), len(srv.names))
	}

	byName := map[string]map[string]interface{}{}
	for _, rt := range rawTools {
		tool := rt.(map[string]interface{})
		byName[tool["name"].(string)] = tool
	}

	// Every allowlisted command must appear and never expose a mutator.
	for _, name := range mcpReadOnlyTools {
		if _, ok := byName[name]; !ok {
			t.Errorf("tool %q missing from tools/list", name)
		}
	}
	for _, mutator := range []string{"create", "insert", "replace", "move", "delete", "rename"} {
		if _, ok := byName[mutator]; ok {
			t.Errorf("mutator %q must not be exposed", mutator)
		}
	}

	// The generated inputSchema must match each backing command's flags
	// (skipping forced/internal flags) plus the positional args array.
	for _, name := range srv.names {
		cmd := srv.tools[name]
		tool := byName[name]
		schema, _ := tool["inputSchema"].(map[string]interface{})
		props, _ := schema["properties"].(map[string]interface{})

		want := map[string]bool{}
		if cmd.MaxArgs != 0 {
			want["args"] = true
		}
		for flag, takesValue := range cmd.Flags {
			if mcpSkipFlags[flag] {
				continue
			}
			key := flagParamName(flag)
			want[key] = true
			// Value flags are strings; boolean flags are booleans.
			p, _ := props[key].(map[string]interface{})
			gotType := p["type"]
			if takesValue && gotType != "string" {
				t.Errorf("%s: flag %s type = %v, want string", name, flag, gotType)
			}
			if !takesValue && gotType != "boolean" {
				t.Errorf("%s: flag %s type = %v, want boolean", name, flag, gotType)
			}
		}
		if len(props) != len(want) {
			t.Errorf("%s: schema has %d properties %v, want %d %v", name, len(props), keysOf(props), len(want), want)
		}
		// --json is forced, never a parameter.
		if _, leaked := props["json"]; leaked {
			t.Errorf("%s: --json must not be a tool parameter", name)
		}
	}
}

func keysOf(m map[string]interface{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestMCPToolsCallCallgraph(t *testing.T) {
	t.Chdir(t.TempDir())
	writeTempGo(t, ".", "x.go", mcpTestSrc)

	srv := newTestServer()
	resp := roundTrip(t, srv, map[string]interface{}{
		"jsonrpc": "2.0", "id": 3, "method": "tools/call",
		"params": map[string]interface{}{
			"name": "callgraph",
			"arguments": map[string]interface{}{
				"args":  []string{"Top"},
				"depth": "3",
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("tools/call error: %+v", resp.Error)
	}
	result, _ := resp.Result.(map[string]interface{})
	if result["isError"] == true {
		t.Fatalf("tool reported error: %+v", result)
	}
	text := toolText(t, result)
	// --json is forced, so the result must be valid JSON mentioning the callees.
	if !json.Valid([]byte(text)) {
		t.Errorf("callgraph output is not JSON (forced --json failed):\n%s", text)
	}
	for _, want := range []string{"Top", "Middle", "Leaf"} {
		if !strings.Contains(text, want) {
			t.Errorf("callgraph JSON missing %q:\n%s", want, text)
		}
	}
}

func TestMCPToolsCallErrorIsToolError(t *testing.T) {
	t.Chdir(t.TempDir())
	writeTempGo(t, ".", "x.go", mcpTestSrc)

	srv := newTestServer()
	resp := roundTrip(t, srv, map[string]interface{}{
		"jsonrpc": "2.0", "id": 4, "method": "tools/call",
		"params": map[string]interface{}{
			"name":      "callgraph",
			"arguments": map[string]interface{}{"args": []string{"DoesNotExist"}},
		},
	})
	// A command failure is surfaced as a tool error, not a JSON-RPC error.
	if resp.Error != nil {
		t.Fatalf("expected tool-level error, got JSON-RPC error: %+v", resp.Error)
	}
	result, _ := resp.Result.(map[string]interface{})
	if result["isError"] != true {
		t.Errorf("expected isError=true for missing target, got %+v", result)
	}
}

func TestMCPUnknownTool(t *testing.T) {
	srv := newTestServer()
	resp := roundTrip(t, srv, map[string]interface{}{
		"jsonrpc": "2.0", "id": 5, "method": "tools/call",
		"params": map[string]interface{}{"name": "nope", "arguments": map[string]interface{}{}},
	})
	if resp.Error == nil {
		t.Fatalf("expected JSON-RPC error for unknown tool")
	}
	if resp.Error.Code != rpcInvalidParams {
		t.Errorf("error code = %d, want %d", resp.Error.Code, rpcInvalidParams)
	}
}

func TestMCPUnknownMethod(t *testing.T) {
	srv := newTestServer()
	resp := roundTrip(t, srv, map[string]interface{}{
		"jsonrpc": "2.0", "id": 6, "method": "bogus/method",
	})
	if resp.Error == nil || resp.Error.Code != rpcMethodNotFound {
		t.Fatalf("expected method-not-found error, got %+v", resp.Error)
	}
}

func toolText(t *testing.T, result map[string]interface{}) string {
	t.Helper()
	content, _ := result["content"].([]interface{})
	if len(content) == 0 {
		t.Fatalf("tool result has no content: %+v", result)
	}
	first, _ := content[0].(map[string]interface{})
	return first["text"].(string)
}
