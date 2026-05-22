package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveAgentRuleTargets(t *testing.T) {
	cases := []struct {
		target string
		want   []string
	}{
		{"claude.md", []string{"CLAUDE.md"}},
		{"CLAUDE", []string{"CLAUDE.md"}},
		{"cursor", []string{".cursorrules"}},
		{".cursorrules", []string{".cursorrules"}},
		{"agents.md", []string{"AGENTS.md"}},
		{"all", []string{"CLAUDE.md", ".cursorrules", "AGENTS.md"}},
		{"nonsense", nil},
	}
	for _, c := range cases {
		got := resolveAgentRuleTargets(c.target)
		if len(got) != len(c.want) {
			t.Errorf("resolveAgentRuleTargets(%q) = %v, want %v", c.target, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("resolveAgentRuleTargets(%q)[%d] = %q, want %q", c.target, i, got[i], c.want[i])
			}
		}
	}
}

func TestWriteAgentRules_NewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")
	if err := writeAgentRules(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), agentRulesSentinel) {
		t.Errorf("new file missing sentinel: %s", path)
	}
	if !strings.Contains(string(data), "gorefactor") {
		t.Errorf("new file missing template body")
	}
}

func TestWriteAgentRules_AppendsToExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")
	pre := "# Project Notes\n\nSome existing content.\n"
	if err := os.WriteFile(path, []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeAgentRules(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	s := string(data)
	if !strings.HasPrefix(s, "# Project Notes") {
		t.Errorf("append clobbered existing content: %q", s[:min(80, len(s))])
	}
	if !strings.Contains(s, agentRulesSentinel) {
		t.Errorf("append did not insert sentinel")
	}
}

func TestWriteAgentRules_Idempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")
	if err := writeAgentRules(path); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(path)
	if err := writeAgentRules(path); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(path)
	if string(first) != string(second) {
		t.Errorf("re-running mutated the file (first=%d bytes, second=%d bytes)", len(first), len(second))
	}
}

func TestInitAgentRulesCommand_UnknownTarget(t *testing.T) {
	err := initAgentRulesCommand([]string{"--target", "obscure"})
	if err == nil {
		t.Errorf("expected error for unknown --target")
	}
}

func TestInitAgentRulesCommand_All(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := initAgentRulesCommand([]string{"--target", "all"}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"CLAUDE.md", ".cursorrules", "AGENTS.md"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Errorf("missing %s: %v", name, err)
			continue
		}
		if !strings.Contains(string(data), agentRulesSentinel) {
			t.Errorf("%s missing sentinel", name)
		}
	}
}
