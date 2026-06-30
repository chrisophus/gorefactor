package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
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

// connectTestClient wires an SDK client to the gorefactor server over an
// in-memory transport and returns the connected client session. allowWrite
// selects whether the mutation guides are registered (Phase 3).
func connectTestClient(t *testing.T, allowWrite bool) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	server := newMCPServer(getCommands(), allowWrite)
	clientT, serverT := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, serverT, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	cs, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func TestMCPInitializeServerInfo(t *testing.T) {
	cs := connectTestClient(t, false)
	got := cs.InitializeResult()
	if got == nil || got.ServerInfo == nil {
		t.Fatalf("missing initialize result/serverInfo")
	}
	if got.ServerInfo.Name != "gorefactor" {
		t.Errorf("serverInfo.name = %q, want gorefactor", got.ServerInfo.Name)
	}
}

func TestMCPToolsListSchema(t *testing.T) {
	cs := connectTestClient(t, false)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	byName := map[string]*mcp.Tool{}
	for _, tool := range res.Tools {
		byName[tool.Name] = tool
	}

	// Every allowlisted command must appear, and no mutator may leak.
	for _, name := range mcpReadOnlyTools {
		tool, ok := byName[name]
		if !ok {
			t.Errorf("tool %q missing from tools/list", name)
			continue
		}
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("tool %q should carry ReadOnlyHint", name)
		}
	}
	for _, mutator := range []string{"create", "insert", "replace", "move", "delete", "rename"} {
		if _, ok := byName[mutator]; ok {
			t.Errorf("mutator %q must not be exposed", mutator)
		}
	}

	// The generated inputSchema must match each backing command's flags
	// (skipping forced/internal flags) plus the positional args array.
	for name, tool := range byName {
		cmd := getCommands()[name]
		props := schemaProperties(t, tool.InputSchema)

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
			gotType := props[key]["type"]
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
		if _, leaked := props["json"]; leaked {
			t.Errorf("%s: --json must not be a tool parameter", name)
		}
	}
}

// schemaProperties decodes a tool's inputSchema (delivered to the client as a
// map[string]any) into a properties map keyed by property name.
func schemaProperties(t *testing.T, schema any) map[string]map[string]interface{} {
	t.Helper()
	b, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("marshal schema: %v", err)
	}
	var decoded struct {
		Properties map[string]map[string]interface{} `json:"properties"`
	}
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}
	if decoded.Properties == nil {
		return map[string]map[string]interface{}{}
	}
	return decoded.Properties
}

func keysOf(m map[string]map[string]interface{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestMCPCallToolCallgraph(t *testing.T) {
	t.Chdir(t.TempDir())
	writeTempGo(t, ".", "x.go", mcpTestSrc)

	cs := connectTestClient(t, false)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "callgraph",
		Arguments: map[string]interface{}{
			"args":  []string{"Top"},
			"depth": "3",
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool reported error: %s", toolText(t, res))
	}
	text := toolText(t, res)
	// --json is forced, so the result must be valid JSON naming the callees.
	if !json.Valid([]byte(text)) {
		t.Errorf("callgraph output is not JSON (forced --json failed):\n%s", text)
	}
	for _, want := range []string{"Top", "Middle", "Leaf"} {
		if !strings.Contains(text, want) {
			t.Errorf("callgraph JSON missing %q:\n%s", want, text)
		}
	}
}

func TestMCPCallToolErrorIsToolError(t *testing.T) {
	t.Chdir(t.TempDir())
	writeTempGo(t, ".", "x.go", mcpTestSrc)

	cs := connectTestClient(t, false)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "callgraph",
		Arguments: map[string]interface{}{"args": []string{"DoesNotExist"}},
	})
	// A command failure is surfaced as a tool error, not a protocol error.
	if err != nil {
		t.Fatalf("expected tool-level error, got protocol error: %v", err)
	}
	if !res.IsError {
		t.Errorf("expected IsError=true for missing target, got: %s", toolText(t, res))
	}
}

func TestMCPCallUnknownTool(t *testing.T) {
	cs := connectTestClient(t, false)
	_, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "nope",
		Arguments: map[string]interface{}{},
	})
	if err == nil {
		t.Fatalf("expected protocol error for unknown tool")
	}
}

func toolText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if len(res.Content) == 0 {
		t.Fatalf("tool result has no content")
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("first content is %T, want *mcp.TextContent", res.Content[0])
	}
	return tc.Text
}
