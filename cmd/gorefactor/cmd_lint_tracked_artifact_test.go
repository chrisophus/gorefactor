package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestTrackedArtifact_FlagsBinariesAndCoverage(t *testing.T) {
	binary := append([]byte("\x7fELF"), make([]byte, 64)...) // NUL bytes inside
	dir := initTestGitRepo(t, map[string][]byte{
		"main.go":              []byte("package main\n"),
		"tool":                 binary,
		"coverage.html":        []byte("<html>coverage</html>"),
		"pkg/testdata/fixture": binary, // legitimate binary fixture
	})
	issues := trackedArtifactRule{}.Run(LintContext{Root: dir})
	byFile := map[string]string{}
	for _, iss := range issues {
		if iss.Rule != "tracked-artifact" || iss.Severity != "warning" {
			t.Errorf("issue has rule=%q severity=%q, want tracked-artifact/warning", iss.Rule, iss.Severity)
		}
		byFile[filepath.Base(iss.File)] = iss.Message
	}
	if len(issues) != 2 {
		t.Fatalf("got %d issues, want 2 (binary + coverage.html): %+v", len(issues), issues)
	}
	if msg := byFile["tool"]; !strings.Contains(msg, "binary") {
		t.Errorf("binary finding missing or mis-worded: %q", msg)
	}
	if msg := byFile["coverage.html"]; !strings.Contains(msg, "coverage") {
		t.Errorf("coverage finding missing or mis-worded: %q", msg)
	}
	if _, flagged := byFile["fixture"]; flagged {
		t.Error("testdata binary fixture was flagged; testdata must be exempt")
	}
}

func TestTrackedArtifact_UntrackedBinaryIgnored(t *testing.T) {
	dir := initTestGitRepo(t, map[string][]byte{"main.go": []byte("package main\n")})
	// Present on disk but never git-added: not the rule's business.
	if err := os.WriteFile(filepath.Join(dir, "local-build"), append([]byte{0}, []byte("bin")...), 0o644); err != nil {
		t.Fatal(err)
	}
	issues := trackedArtifactRule{}.Run(LintContext{Root: dir})
	if len(issues) != 0 {
		t.Fatalf("untracked binary flagged: %+v", issues)
	}
}

func TestTrackedArtifact_SelfSkipsOutsideGitRepo(t *testing.T) {
	dir := t.TempDir() // no git repo here
	issues := trackedArtifactRule{}.Run(LintContext{Root: dir})
	if issues != nil {
		t.Fatalf("expected nil outside a git repo, got %+v", issues)
	}
}

// TestTrackedArtifact_CleanOnThisRepo is the dogfooding guard: the repo that
// ships this rule must itself stay free of tracked artifacts.
func TestTrackedArtifact_CleanOnThisRepo(t *testing.T) {
	issues := trackedArtifactRule{}.Run(LintContext{Root: filepath.Join("..", "..")})
	for _, iss := range issues {
		t.Errorf("tracked artifact in tree: %s: %s", iss.File, iss.Message)
	}
}

// initTestGitRepo creates a temp git repo with the given files committed and
// returns its root. Skips the test when git is unavailable, mirroring the
// rule's own self-skip.
func initTestGitRepo(t *testing.T, files map[string][]byte) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	for rel, content := range files {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("add", "-A")
	run("commit", "-q", "-m", "fixture")
	return dir
}
