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

func TestComputeScore(t *testing.T) {
	clean := &Report{}
	clean.ComputeScore()
	if clean.Score == nil || *clean.Score != 100 {
		t.Fatalf("clean tree score = %v, want 100", clean.Score)
	}
	dirty := &Report{Findings: []Finding{
		{Severity: SeverityError},
		{Severity: SeverityWarning},
		{Severity: SeverityInfo},
	}}
	dirty.ComputeScore()
	if dirty.Score == nil || *dirty.Score >= 100 || *dirty.Score <= 0 {
		t.Fatalf("dirty tree score = %v, want in (0, 100)", dirty.Score)
	}
	worse := &Report{Findings: make([]Finding, 100)}
	for i := range worse.Findings {
		worse.Findings[i].Severity = SeverityError
	}
	worse.ComputeScore()
	if *worse.Score >= *dirty.Score {
		t.Fatalf("score must decrease with findings: %v >= %v", *worse.Score, *dirty.Score)
	}
	ranking := &Report{Findings: []Finding{
		{Severity: SeverityInfo, Rule: "high-blast-radius"},
		{Severity: SeverityInfo, Rule: "low-gorefactor-adherence"},
	}}
	ranking.ComputeScore()
	if *ranking.Score != 100 {
		t.Fatalf("ranking-signal rules must not depress the score: %v", *ranking.Score)
	}
}

func TestScoreWeightTiers(t *testing.T) {
	cases := []struct {
		rule string
		sev  Severity
		want float64
	}{
		{"duplicate-block", SeverityWarning, 1},
		{"error-not-wrapped", SeverityWarning, 1},
		{"deep-nesting", SeverityWarning, 0.5},
		{"excessive-params", SeverityInfo, 0.125},
		{"funcorder-function", SeverityWarning, 0},
		{"high-blast-radius", SeverityInfo, 0},
		{"untested-package", SeverityInfo, 1},
		{"untested-function", SeverityInfo, 0},
		{"golangci/dupl", SeverityWarning, 1},
		{"govulncheck/GO-2024-0001", SeverityError, 3},
	}
	for _, c := range cases {
		if got := scoreWeight(c.rule, c.sev); got != c.want {
			t.Errorf("scoreWeight(%q, %s) = %v, want %v", c.rule, c.sev, got, c.want)
		}
	}

	defect := &Report{Findings: []Finding{{Rule: "duplicate-block", Severity: SeverityWarning}}}
	proxy := &Report{Findings: []Finding{{Rule: "deep-nesting", Severity: SeverityWarning}}}
	defect.ComputeScore()
	proxy.ComputeScore()
	if !(*proxy.Score > *defect.Score) {
		t.Fatalf("proxy finding must cost less than defect finding: proxy=%v defect=%v", *proxy.Score, *defect.Score)
	}
}

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
