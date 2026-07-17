package doctor

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// fakeSubstrate is the test double for Diagnose/baseline plumbing.
type fakeSubstrate struct {
	info     SubstrateInfo
	findings []Finding
	// baseFindings, when non-nil, is returned for runs outside the original
	// root (i.e. the baseline worktree).
	baseFindings []Finding
	err          error
	root         string
	probeErr     error
}

func (f *fakeSubstrate) Info() SubstrateInfo { return f.info }

func (f *fakeSubstrate) Run(ctx RunContext) ([]Finding, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.baseFindings != nil && f.root != "" && ctx.Root != f.root {
		return append([]Finding(nil), f.baseFindings...), nil
	}
	return append([]Finding(nil), f.findings...), nil
}

func (f *fakeSubstrate) Probe(string) error { return f.probeErr }

// gitRepo creates a temp dir with one committed Go file and chdirs into it.
func gitRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	// Resolve symlinks (macOS /var -> /private/var) so path comparisons hold.
	if r, err := filepath.EvalSymlinks(dir); err == nil {
		dir = r
	}
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitRun(t, dir, "init", "-q")
	gitRun(t, dir, "config", "user.email", "t@e.com")
	gitRun(t, dir, "config", "user.name", "t")
	gitRun(t, dir, "config", "commit.gpgsign", "false")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-q", "-m", "init")
	return dir
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = gitScrubbedEnv() // hook-injected GIT_* vars must not leak into fixtures
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
