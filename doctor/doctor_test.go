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
	clean.ComputeScore(1000)
	if clean.Score == nil || *clean.Score != 100 {
		t.Fatalf("clean tree score = %v, want 100", clean.Score)
	}
	dirty := &Report{Findings: []Finding{
		{Severity: SeverityError},
		{Severity: SeverityWarning},
		{Severity: SeverityInfo},
	}}
	dirty.ComputeScore(1000)
	if dirty.Score == nil || *dirty.Score >= 100 || *dirty.Score <= 0 {
		t.Fatalf("dirty tree score = %v, want in (0, 100)", dirty.Score)
	}
	worse := &Report{Findings: make([]Finding, 100)}
	for i := range worse.Findings {
		worse.Findings[i].Severity = SeverityError
	}
	worse.ComputeScore(1000)
	if *worse.Score >= *dirty.Score {
		t.Fatalf("score must decrease with findings: %v >= %v", *worse.Score, *dirty.Score)
	}
	ranking := &Report{Findings: []Finding{
		{Severity: SeverityInfo, Rule: "high-blast-radius"},
		{Severity: SeverityInfo, Rule: "low-gorefactor-adherence"},
	}}
	ranking.ComputeScore(1000)
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
		{"long-function", SeverityWarning, 0.5},
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
	proxy := &Report{Findings: []Finding{{Rule: "long-function", Severity: SeverityWarning}}}
	defect.ComputeScore(1000)
	proxy.ComputeScore(1000)
	if !(*proxy.Score > *defect.Score) {
		t.Fatalf("proxy finding must cost less than defect finding: proxy=%v defect=%v", *proxy.Score, *defect.Score)
	}
}

// TestComputeScoreSizeNormalization pins the size-relative proxy scoring: the
// same proxy finding count costs less on a larger codebase (it is a density,
// not an absolute count), while a defect costs the same regardless of size, and
// a codebase at the reference size scores as the pre-normalisation model did.
func TestComputeScoreSizeNormalization(t *testing.T) {
	proxies := func() []Finding {
		fs := make([]Finding, 20)
		for i := range fs {
			fs[i] = Finding{Rule: "long-function", Severity: SeverityWarning}
		}
		return fs
	}

	small := &Report{Findings: proxies()}
	large := &Report{Findings: proxies()}
	small.ComputeScore(1000)  // reference size: no scaling
	large.ComputeScore(10000) // 10x functions -> proxy density 1/10
	if !(*large.Score > *small.Score) {
		t.Fatalf("proxy findings must cost less on a larger codebase: large=%v small=%v", *large.Score, *small.Score)
	}

	// Defects are absolute: same finding count, same score regardless of size.
	dSmall := &Report{Findings: []Finding{{Rule: "duplicate-block", Severity: SeverityWarning}}}
	dLarge := &Report{Findings: []Finding{{Rule: "duplicate-block", Severity: SeverityWarning}}}
	dSmall.ComputeScore(1000)
	dLarge.ComputeScore(50000)
	if *dSmall.Score != *dLarge.Score {
		t.Fatalf("defect score must not depend on codebase size: %v vs %v", *dSmall.Score, *dLarge.Score)
	}

	// Below the reference size, no leniency (floored): a tiny codebase with the
	// same proxy count scores the same as one at the reference size.
	tiny := &Report{Findings: proxies()}
	tiny.ComputeScore(200)
	if *tiny.Score != *small.Score {
		t.Fatalf("sub-reference sizes must floor (no density benefit): tiny=%v ref=%v", *tiny.Score, *small.Score)
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
