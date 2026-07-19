package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestOrphanedConfigPath_RenameProducesFinding is harness-integrity plan item
// 3's acceptance criterion: renaming an exempted file without updating the
// config produces a finding in the same commit.
func TestOrphanedConfigPath_RenameProducesFinding(t *testing.T) {
	dir := writeLivenessFixture(t, map[string]string{
		".golangci.yml":    livenessGolangci,
		"pkg/exempted.go":  "package pkg\n",
		"fixtures/data.go": "package fixtures\n",
	})
	if issues := (orphanedConfigPathRule{}).Run(LintContext{Root: dir}); len(issues) != 0 {
		t.Fatalf("live config produced findings: %+v", issues)
	}
	// The rename: the exempted file disappears, the config is not updated.
	if err := os.Rename(filepath.Join(dir, "pkg/exempted.go"), filepath.Join(dir, "pkg/renamed.go")); err != nil {
		t.Fatal(err)
	}
	issues := (orphanedConfigPathRule{}).Run(LintContext{Root: dir})
	if len(issues) != 1 {
		t.Fatalf("got %d findings after rename, want 1: %+v", len(issues), issues)
	}
	if !strings.Contains(issues[0].Message, "pkg/exempted") {
		t.Errorf("finding does not name the orphaned pattern: %s", issues[0].Message)
	}
}

const livenessGolangci = `version: "2"
linters:
  exclusions:
    rules:
      - linters: [funlen]
        path: pkg/exempted\.go$
    paths:
      - ^fixtures/
`

// TestOrphanedConfigPath_UnwalkedDirsAreNotOrphans pins that the liveness
// scan sees the whole tree, not lint's Go-file walk: an exemption pointing
// at a fixture directory the linter never analyzes is still live.
func TestOrphanedConfigPath_UnwalkedDirsAreNotOrphans(t *testing.T) {
	dir := writeLivenessFixture(t, map[string]string{
		".golangci.yml":     livenessGolangci,
		"pkg/exempted.go":   "package pkg\n",
		"fixtures/data.txt": "not even a go file\n",
	})
	if issues := (orphanedConfigPathRule{}).Run(LintContext{Root: dir}); len(issues) != 0 {
		t.Fatalf("fixture-dir exemption flagged as orphaned: %+v", issues)
	}
}

func TestOrphanedConfigPath_BaselineEntries(t *testing.T) {
	baseline := `{"version":1,"issues":[
		{"fingerprint":"a","file":"pkg/live.go","rule":"long-function","count":1},
		{"fingerprint":"b","file":"pkg/live.go:12:1","rule":"funcorder-function","count":1},
		{"fingerprint":"c","file":"pkg/gone.go","rule":"complexity","count":1}
	]}`
	dir := writeLivenessFixture(t, map[string]string{
		defaultBaselinePath: baseline,
		"pkg/live.go":       "package pkg\n",
	})
	issues := (orphanedConfigPathRule{}).Run(LintContext{Root: dir})
	if len(issues) != 1 {
		t.Fatalf("got %d findings, want 1 (only pkg/gone.go): %+v", len(issues), issues)
	}
	if !strings.Contains(issues[0].Message, "pkg/gone.go") {
		t.Errorf("finding does not name the missing file: %s", issues[0].Message)
	}
}

func TestTrimLineColSuffix(t *testing.T) {
	cases := []struct{ in, want string }{
		{"a/b.go", "a/b.go"},
		{"a/b.go:171:1", "a/b.go"},
		{"a/b.go:171", "a/b.go"},
		{"a/b.go:x:1", "a/b.go:x:1"},
	}
	for _, tc := range cases {
		if got := trimLineColSuffix(tc.in); got != tc.want {
			t.Errorf("trimLineColSuffix(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func writeLivenessFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}
