package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

//go:embed assets/agent-rules.md
var agentRulesTemplate string

const agentRulesSentinel = "<!-- gorefactor:agent-rules -->"

// mcpClientConfigPath is the conventional per-project MCP client config file
// (used by Claude Code and compatible clients).
const mcpClientConfigPath = ".mcp.json"

func initAgentRulesCommand(args []string) error {
	target := "claude.md"
	writeMCP := false
	mcpOnly := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--target":
			if i+1 < len(args) {
				target = args[i+1]
				i++
			}
		case strings.HasPrefix(a, "--target="):
			target = strings.TrimPrefix(a, "--target=")
		case a == "--mcp":
			writeMCP = true
		case a == "--mcp-only":
			mcpOnly = true
			writeMCP = true
		}
	}

	if !mcpOnly {
		paths := resolveAgentRuleTargets(target)
		if len(paths) == 0 {
			return fmt.Errorf("unknown --target %q (want: claude.md, cursor, agents.md, all)", target)
		}
		for _, path := range paths {
			if err := writeAgentRules(path); err != nil {
				return err
			}
		}
	}

	if writeMCP {
		if err := writeMCPClientConfig(mcpClientConfigPath); err != nil {
			return err
		}
	}
	return nil
}

// writeMCPClientConfig merges a "gorefactor" entry into the project's
// .mcp.json so an MCP client launches `gorefactor mcp`. An existing file's
// other servers are preserved; an existing gorefactor entry is left untouched
// (so a user's customised args/flags survive re-running the installer).
func writeMCPClientConfig(path string) error {
	config := map[string]interface{}{}
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(existing) > 0 {
		if err := json.Unmarshal(existing, &config); err != nil {
			return fmt.Errorf("%s is not valid JSON: %w", path, err)
		}
	}

	servers, _ := config["mcpServers"].(map[string]interface{})
	if servers == nil {
		servers = map[string]interface{}{}
	}
	if _, ok := servers["gorefactor"]; ok {
		fmt.Printf("%s: already configures the gorefactor MCP server (skipping)\n", path)
		return nil
	}
	servers["gorefactor"] = map[string]interface{}{
		"command": "gorefactor",
		"args":    []string{"mcp"},
	}
	config["mcpServers"] = servers

	out, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return err
	}
	fmt.Printf("%s: wrote gorefactor MCP server config\n", path)
	return nil
}

func resolveAgentRuleTargets(target string) []string {
	switch strings.ToLower(target) {
	case "all":
		return []string{"CLAUDE.md", ".cursorrules", "AGENTS.md"}
	case "claude.md", "claude":
		return []string{"CLAUDE.md"}
	case "cursor", ".cursorrules":
		return []string{".cursorrules"}
	case "agents.md", "agents":
		return []string{"AGENTS.md"}
	default:
		return nil
	}
}

func writeAgentRules(path string) error {
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if strings.Contains(string(existing), agentRulesSentinel) {
		fmt.Printf("%s: already contains gorefactor agent-rules (skipping)\n", path)
		return nil
	}
	var content string
	if len(existing) == 0 {
		content = agentRulesTemplate
	} else {
		content = strings.TrimRight(string(existing), "\n") + "\n\n" + agentRulesTemplate
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return err
	}
	fmt.Printf("%s: wrote gorefactor agent-rules\n", path)
	return nil
}
