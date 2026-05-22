package main

import (
	_ "embed"
	"fmt"
	"os"
	"strings"
)

//go:embed assets/agent-rules.md
var agentRulesTemplate string

const agentRulesSentinel = "<!-- gorefactor:agent-rules -->"

func initAgentRulesCommand(args []string) error {
	target := "claude.md"
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
		}
	}

	paths := resolveAgentRuleTargets(target)
	if len(paths) == 0 {
		return fmt.Errorf("unknown --target %q (want: claude.md, cursor, agents.md, all)", target)
	}

	for _, path := range paths {
		if err := writeAgentRules(path); err != nil {
			return err
		}
	}
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
