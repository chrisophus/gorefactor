package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chrisophus/gorefactor/analyzer"
)

// changedFixtureBase is the committed state of a tiny module the changed-mode
// tests run against.
var changedFixtureBase = map[string]string{
	"go.mod": "module example.com/changed\n\ngo 1.21\n",
	"order.go": `package changed

// Total sums a bill.
func Total(cents int) int {
	return cents
}
`,
}

const changedFixtureEdit = `package changed

// Total sums a bill.
func Total(cents int) int {
	if cents < 0 {
		return 0
	}
	return cents
}
`

func TestContextChangedEmitsEnvelope(t *testing.T) {
	dir := changedFixtureRepo(t)
	out := captureStdout(t, func() {
		if err := contextCommand([]string{"--changed", "HEAD", "--in", dir, "--json"}); err != nil {
			t.Errorf("context --changed: %v", err)
		}
	})
	var payload struct {
		SchemaVersion int `json:"schemaVersion"`
		Provider      struct {
			Name     string `json:"name"`
			Language string `json:"language"`
		} `json:"provider"`
		Files []struct {
			Path    string   `json:"path"`
			Class   string   `json:"class"`
			Symbols []string `json:"symbols"`
		} `json:"files"`
		Expansions []struct {
			Role    string `json:"role"`
			Symbol  string `json:"symbol"`
			Content string `json:"content"`
		} `json:"expansions"`
		PromptFragment string `json:"promptFragment"`
	}
	decodeEnvelope(t, out, &payload)

	if payload.SchemaVersion != 1 || payload.Provider.Name != "gorefactor" || payload.Provider.Language != "go" {
		t.Errorf("envelope frame = %d %+v", payload.SchemaVersion, payload.Provider)
	}
	if len(payload.Files) != 1 || payload.Files[0].Path != "order.go" || payload.Files[0].Class != "source" {
		t.Fatalf("files = %+v", payload.Files)
	}
	if len(payload.Files[0].Symbols) != 1 || payload.Files[0].Symbols[0] != "Total" {
		t.Errorf("symbols = %v, want [Total]", payload.Files[0].Symbols)
	}
	if len(payload.Expansions) == 0 || payload.Expansions[0].Role != "enclosing" {
		t.Fatalf("expansions = %+v", payload.Expansions)
	}
	if !strings.Contains(payload.Expansions[0].Content, "func Total(cents int) int {") {
		t.Errorf("enclosing expansion is not the whole function:\n%s", payload.Expansions[0].Content)
	}
	if payload.PromptFragment == "" {
		t.Error("envelope carries no prompt fragment")
	}
}

func TestContextChangedHumanSummary(t *testing.T) {
	dir := changedFixtureRepo(t)
	out := captureStdout(t, func() {
		if err := contextCommand([]string{"--changed", "HEAD", "--in", dir}); err != nil {
			t.Errorf("context --changed: %v", err)
		}
	})
	for _, want := range []string{"context envelope v1", "files: 1 (source 1)", "expansions:"} {
		if !strings.Contains(out, want) {
			t.Errorf("summary missing %q:\n%s", want, out)
		}
	}
}

func TestContextArgumentRules(t *testing.T) {
	// The symbol argument is required only when --changed is absent.
	assertExitCode(t, contextCommand(nil), exitUsage)
	if err := checkCommandArgs(getCommands()["context"], []string{"--changed", "HEAD"}); err != nil {
		t.Errorf("context --changed REF rejected before it ran: %v", err)
	}
	assertExitCode(t, contextCommand([]string{"--changed", "HEAD", "Total"}), exitUsage)
	assertExitCode(t, contextCommand([]string{"--changed", "HEAD", "--budget", "500"}), exitUsage)
}

// TestContextChangedIsDeterministic guards the property the consumer builds
// its evals on.
func TestContextChangedIsDeterministic(t *testing.T) {
	dir := changedFixtureRepo(t)
	runOnce := func() string {
		return captureStdout(t, func() {
			if err := contextCommand([]string{"--changed", "HEAD", "--in", dir, "--json"}); err != nil {
				t.Errorf("context --changed: %v", err)
			}
		})
	}
	first, second := runOnce(), runOnce()
	if first != second {
		t.Errorf("two runs differ:\n%s\n\n%s", first, second)
	}
	var check map[string]any
	if err := json.Unmarshal([]byte(first), &check); err != nil {
		t.Fatalf("output is not JSON: %v", err)
	}
}

// changedFixtureRepo commits the module and leaves one function edited.
func changedFixtureRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	t.Setenv("GOWORK", "off")
	dir := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	for rel, content := range changedFixtureBase {
		if err := os.WriteFile(filepath.Join(dir, rel), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = analyzer.SanitizedGitEnv()
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("init", "-q")
	run("add", "-A")
	run("-c", "user.email=fixture@example.com", "-c", "user.name=Fixture",
		"-c", "commit.gpgsign=false", "commit", "-q", "-m", "base")
	if err := os.WriteFile(filepath.Join(dir, "order.go"), []byte(changedFixtureEdit), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}
