package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGolangciLintRuleReportsToolFailureDistinctly(t *testing.T) {
	dir := withGolangciConfig(t)
	installFakeGolangciLint(t, "#!/bin/sh\necho 'fake toolchain mismatch' >&2\nexit 1\n")

	issues := (golangciLintRule{}).Run(LintContext{Root: dir})
	if len(issues) != 1 {
		t.Fatalf("expected exactly 1 issue, got %d: %+v", len(issues), issues)
	}
	if issues[0].Rule != golangciToolFailureRule {
		t.Fatalf("expected Rule=%q for a tool-execution failure, got %q", golangciToolFailureRule, issues[0].Rule)
	}
	if !strings.Contains(issues[0].Message, "fake toolchain mismatch") {
		t.Fatalf("expected message to include the underlying stderr, got %q", issues[0].Message)
	}
}

func TestGolangciLintRuleReportsRealFindingsNormally(t *testing.T) {
	dir := withGolangciConfig(t)
	fakeJSON := `{"Issues":[{"FromLinter":"errcheck","Text":"unchecked error","Severity":"","Pos":{"Filename":"main.go","Line":10}}]}`
	installFakeGolangciLint(t, "#!/bin/sh\ncat <<'JSON'\n"+fakeJSON+"\nJSON\n")

	issues := (golangciLintRule{}).Run(LintContext{Root: dir})
	if len(issues) != 1 || issues[0].Rule != "golangci-lint" {
		t.Fatalf("expected 1 real finding with Rule=golangci-lint, got %+v", issues)
	}
}

func TestDoctorGolangciStageSoftSkipsOnToolFailure(t *testing.T) {
	dir := withGolangciConfig(t)
	installFakeGolangciLint(t, "#!/bin/sh\necho 'boom' >&2\nexit 1\n")

	stage := doctorGolangciStage(dir)
	// A tool that can't run at all must not gate the commit/doctor run — it
	// can't be told apart from "clean" by this stage, so it gets the same
	// soft-skip treatment as a missing binary. CI runs a known-good
	// golangci-lint and is the real enforcement backstop.
	if !stage.ok {
		t.Fatal("stage should soft-skip (ok=true), not hard-fail, when golangci-lint can't run at all")
	}
	if !strings.Contains(stage.info, "did not run") || !strings.Contains(stage.info, "boom") {
		t.Fatalf("expected info to clearly say the tool didn't run and include why, got %q", stage.info)
	}
}

func TestDoctorGolangciStageReportsRealFindingsAsCount(t *testing.T) {
	dir := withGolangciConfig(t)
	fakeJSON := `{"Issues":[{"FromLinter":"errcheck","Text":"unchecked error","Severity":"","Pos":{"Filename":"main.go","Line":10}}]}`
	installFakeGolangciLint(t, "#!/bin/sh\ncat <<'JSON'\n"+fakeJSON+"\nJSON\n")

	stage := doctorGolangciStage(dir)
	if stage.ok {
		t.Fatal("stage should be red when there's a real finding")
	}
	if stage.info != "1 issue(s)" {
		t.Fatalf("expected a plain issue count for real findings, got %q", stage.info)
	}
}

// installFakeGolangciLint puts a fake golangci-lint script first on PATH for
// the duration of the test, so golangciLintRule's subprocess-wrapping
// behavior can be tested deterministically regardless of what's actually
// installed in the environment running the test.
func installFakeGolangciLint(t *testing.T, script string) {
	t.Helper()
	binDir := t.TempDir()
	path := filepath.Join(binDir, "golangci-lint")
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("write fake golangci-lint: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func withGolangciConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFileT(t, filepath.Join(dir, ".golangci.yml"), "version: \"2\"\n")
	return dir
}
