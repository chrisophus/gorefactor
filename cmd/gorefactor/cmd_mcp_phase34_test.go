package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// --- Phase 3: mutation tools behind --allow-write ---

func TestMCPWriteToolsAbsentByDefault(t *testing.T) {
	cs := connectTestClient(t, false)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	byName := map[string]*mcp.Tool{}
	for _, tool := range res.Tools {
		byName[tool.Name] = tool
	}
	for _, name := range mcpWriteTools() {
		if _, ok := byName[name]; ok {
			t.Errorf("write tool %q must not be exposed without --allow-write", name)
		}
	}
}

func TestMCPWriteToolsRegisteredWithAllowWrite(t *testing.T) {
	cs := connectTestClient(t, true)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	byName := map[string]*mcp.Tool{}
	for _, tool := range res.Tools {
		byName[tool.Name] = tool
	}

	for _, name := range mcpWriteTools() {
		tool, ok := byName[name]
		if !ok {
			t.Errorf("write tool %q missing with --allow-write", name)
			continue
		}
		if tool.Annotations == nil {
			t.Errorf("write tool %q has no annotations", name)
			continue
		}
		if tool.Annotations.ReadOnlyHint {
			t.Errorf("write tool %q must not carry ReadOnlyHint", name)
		}
		if tool.Annotations.DestructiveHint == nil || !*tool.Annotations.DestructiveHint {
			t.Errorf("write tool %q must carry DestructiveHint=true", name)
		}
		if !strings.Contains(tool.Description, "Modifies Go source") {
			t.Errorf("write tool %q description should flag that it mutates files: %q", name, tool.Description)
		}
	}

	// The read-only tools must still be present alongside the write tools.
	if _, ok := byName["callgraph"]; !ok {
		t.Errorf("read-only tools should remain available in write mode")
	}
}

// TestMCPWriteToolMutatesFile drives an end-to-end edit: the `insert` guide
// adds a function to a file through a tool call, and we confirm the file
// changed on disk.
func TestMCPWriteToolMutatesFile(t *testing.T) {
	t.Chdir(t.TempDir())
	writeTempGo(t, ".", "x.go", mcpTestSrc)

	cs := connectTestClient(t, true)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "insert",
		Arguments: map[string]interface{}{
			"args": []string{"x.go", "at-end", "func Added() {}\n"},
		},
	})
	if err != nil {
		t.Fatalf("CallTool insert: %v", err)
	}
	if res.IsError {
		t.Fatalf("insert tool reported error: %s", toolText(t, res))
	}

	got, err := os.ReadFile("x.go")
	if err != nil {
		t.Fatalf("read back x.go: %v", err)
	}
	if !strings.Contains(string(got), "func Added()") {
		t.Errorf("insert did not add the function:\n%s", got)
	}
}

func TestMCPRequireCleanWorktree(t *testing.T) {
	// Not a git work tree -> error.
	notGit := t.TempDir()
	if err := mcpRequireCleanWorktree(notGit); err == nil {
		t.Errorf("expected error for non-git directory")
	}

	repo := t.TempDir()
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "t"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Skipf("git unavailable: %v: %s", err, out)
		}
	}

	// Empty repo with no changes is clean.
	if err := mcpRequireCleanWorktree(repo); err != nil {
		t.Errorf("empty clean repo should pass, got %v", err)
	}

	// An untracked file makes the tree dirty.
	if err := os.WriteFile(filepath.Join(repo, "dirty.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write dirty file: %v", err)
	}
	if err := mcpRequireCleanWorktree(repo); err == nil {
		t.Errorf("dirty repo should fail the clean-worktree check")
	}
}

// --- Phase 4: resources ---

func TestMCPResourceTemplatesListed(t *testing.T) {
	cs := connectTestClient(t, false)
	res, err := cs.ListResourceTemplates(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListResourceTemplates: %v", err)
	}
	byName := map[string]*mcp.ResourceTemplate{}
	for _, rt := range res.ResourceTemplates {
		byName[rt.Name] = rt
	}
	for _, want := range []string{"skeleton", "inspect", "context"} {
		rt, ok := byName[want]
		if !ok {
			t.Errorf("resource template %q missing", want)
			continue
		}
		if !strings.HasPrefix(rt.URITemplate, mcpResourceScheme+want+"/") {
			t.Errorf("resource %q has unexpected URI template %q", want, rt.URITemplate)
		}
	}
}

func TestMCPReadSkeletonResource(t *testing.T) {
	t.Chdir(t.TempDir())
	writeTempGo(t, ".", "x.go", mcpTestSrc)

	cs := connectTestClient(t, false)
	res, err := cs.ReadResource(context.Background(), &mcp.ReadResourceParams{
		URI: "gorefactor://skeleton/x.go",
	})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if len(res.Contents) == 0 {
		t.Fatalf("resource read returned no contents")
	}
	text := res.Contents[0].Text
	for _, want := range []string{"Top", "Middle", "Leaf"} {
		if !strings.Contains(text, want) {
			t.Errorf("skeleton resource missing %q:\n%s", want, text)
		}
	}
}

func TestMCPReadResourceMissingFileErrors(t *testing.T) {
	t.Chdir(t.TempDir())
	cs := connectTestClient(t, false)
	_, err := cs.ReadResource(context.Background(), &mcp.ReadResourceParams{
		URI: "gorefactor://skeleton/nope.go",
	})
	if err == nil {
		t.Errorf("expected error reading skeleton of a missing file")
	}
}

// --- Phase 4: installer ---

func TestWriteMCPClientConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".mcp.json")

	// Fresh write.
	if err := writeMCPClientConfig(path); err != nil {
		t.Fatalf("writeMCPClientConfig: %v", err)
	}
	servers := readMCPServers(t, path)
	gr, ok := servers["gorefactor"].(map[string]interface{})
	if !ok {
		t.Fatalf("gorefactor server entry missing or wrong type: %v", servers)
	}
	if gr["command"] != "gorefactor" {
		t.Errorf("command = %v, want gorefactor", gr["command"])
	}

	// Re-run preserves a user-customised entry (idempotent: no overwrite).
	custom := `{"mcpServers":{"gorefactor":{"command":"/custom/gorefactor","args":["mcp","--allow-write"]}}}`
	if err := os.WriteFile(path, []byte(custom), 0o644); err != nil {
		t.Fatalf("seed custom config: %v", err)
	}
	if err := writeMCPClientConfig(path); err != nil {
		t.Fatalf("writeMCPClientConfig (rerun): %v", err)
	}
	servers = readMCPServers(t, path)
	gr = servers["gorefactor"].(map[string]interface{})
	if gr["command"] != "/custom/gorefactor" {
		t.Errorf("re-run clobbered a customised gorefactor entry: %v", gr)
	}

	// Existing unrelated servers are preserved on merge.
	if err := os.WriteFile(path, []byte(`{"mcpServers":{"other":{"command":"x"}}}`), 0o644); err != nil {
		t.Fatalf("seed other config: %v", err)
	}
	if err := writeMCPClientConfig(path); err != nil {
		t.Fatalf("writeMCPClientConfig (merge): %v", err)
	}
	servers = readMCPServers(t, path)
	if _, ok := servers["other"]; !ok {
		t.Errorf("merge dropped the unrelated 'other' server: %v", servers)
	}
	if _, ok := servers["gorefactor"]; !ok {
		t.Errorf("merge did not add the gorefactor server: %v", servers)
	}

	// Invalid JSON is reported, not silently overwritten.
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("seed bad json: %v", err)
	}
	if err := writeMCPClientConfig(path); err == nil {
		t.Errorf("expected error for invalid existing .mcp.json")
	}
}

func readMCPServers(t *testing.T, path string) map[string]interface{} {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var cfg struct {
		MCPServers map[string]interface{} `json:"mcpServers"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
	return cfg.MCPServers
}
