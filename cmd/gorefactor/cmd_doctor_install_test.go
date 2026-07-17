package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDoctorInstallWritesRules(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(cwd) }()

	if err := doctorInstallCommand([]string{"--target", "claude.md"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	for _, want := range []string{
		doctorRulesSentinel,
		"vacuous-test",
		"intent api-change",
		"doctor --report --scoped",
		"lint --fix --verify",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("installed rules missing %q", want)
		}
	}
}

func TestDoctorInstallIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(cwd) }()

	path := filepath.Join(dir, "CLAUDE.md")
	if err := os.WriteFile(path, []byte("# Existing instructions\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := doctorInstallCommand(nil); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.HasPrefix(content, "# Existing instructions") {
		t.Error("existing content must be preserved above the section")
	}
	if strings.Count(content, doctorRulesSentinel) != 1 {
		t.Errorf("sentinel count = %d, want 1 (idempotent)", strings.Count(content, doctorRulesSentinel))
	}
}

func TestRenderDoctorRulesTracksRegistry(t *testing.T) {
	content := renderDoctorRules()
	for _, r := range defaultLintRules() {
		if !strings.Contains(content, r.Name()) {
			t.Errorf("rendered rules missing registry rule %s", r.Name())
		}
	}
}

func TestRunnableFixCmd(t *testing.T) {
	cases := map[string]string{
		"":                               "",
		"delete a.go Unused --safe":      "gorefactor delete a.go Unused --safe",
		"gorefactor split a.go --max 10": "gorefactor split a.go --max 10",
		"go mod tidy":                    "go mod tidy",
	}
	for in, want := range cases {
		if got := runnableFixCmd(in); got != want {
			t.Errorf("runnableFixCmd(%q) = %q, want %q", in, got, want)
		}
	}
}
