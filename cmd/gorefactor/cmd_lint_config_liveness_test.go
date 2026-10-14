package main

import (
	"os"
	"os/exec"
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

func TestOrphanedConfigPath_PrunesButRecognizesLiteralNodeModulesPath(t *testing.T) {
	dir := writeLivenessFixture(t, map[string]string{
		".golangci.yml": `version: "2"
linters:
  exclusions:
    paths:
      - ui/node_modules
`,
		"ui/node_modules/vendor/pkg.go": "package vendor\n",
	})
	if issues := (orphanedConfigPathRule{}).Run(LintContext{Root: dir}); len(issues) != 0 {
		t.Fatalf("existing node_modules exemption flagged as orphaned: %+v", issues)
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

// TestOrphanedConfigPath_GitignoredAbsentPathIsNotOrphan pins the marketplace-cp
// shape: an exemption for a gitignored directory that a fresh checkout does not
// contain. The gitignore spellings cover dir-only, dir/* with a re-included
// placeholder, and a plain trailing-slash directory.
func TestOrphanedConfigPath_GitignoredAbsentPathIsNotOrphan(t *testing.T) {
	requireGit(t)
	cases := []struct {
		name      string
		gitignore string
		config    string
	}{
		{"dir-only", "ui/node_modules/\n", "ui/node_modules"},
		{"contents-with-keep", "ui/node_modules/*\n!ui/node_modules/.keep\n", "ui/node_modules"},
		{"scratch-dir", "scratch/\n", "scratch"},
		{"regex-free-path-in-skip-list", "build/\n", "build"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := writeGitLivenessFixture(t, map[string]string{
				".gitignore":    tc.gitignore,
				".golangci.yml": "version: \"2\"\nlinters:\n  exclusions:\n    paths:\n      - " + tc.config + "\n",
				"pkg/live.go":   "package pkg\n",
			})
			if issues := (orphanedConfigPathRule{}).Run(LintContext{Root: dir}); len(issues) != 0 {
				t.Fatalf("gitignored absent path flagged as orphaned: %+v", issues)
			}
		})
	}
}

// TestOrphanedConfigPath_NotIgnoredAbsentPathIsOrphan pins that the gitignore
// exemption is narrow: a missing path the repo does not ignore is still stale,
// even when other paths are ignored.
func TestOrphanedConfigPath_NotIgnoredAbsentPathIsOrphan(t *testing.T) {
	requireGit(t)
	dir := writeGitLivenessFixture(t, map[string]string{
		".gitignore": "scratch/\nui/node_modules/\n",
		".golangci.yml": "version: \"2\"\nlinters:\n  exclusions:\n    paths:\n" +
			"      - scratch\n      - gen/out\n",
		"pkg/live.go": "package pkg\n",
	})
	issues := (orphanedConfigPathRule{}).Run(LintContext{Root: dir})
	if len(issues) != 1 {
		t.Fatalf("got %d findings, want 1 (only gen/out): %+v", len(issues), issues)
	}
	if !strings.Contains(issues[0].Message, "gen/out") {
		t.Errorf("finding does not name the stale path: %s", issues[0].Message)
	}
}

// TestOrphanedConfigPath_ExistingIgnoredPathIsNotOrphan covers the case where
// the gitignored directory is present locally: it is alive by existence.
func TestOrphanedConfigPath_ExistingIgnoredPathIsNotOrphan(t *testing.T) {
	requireGit(t)
	dir := writeGitLivenessFixture(t, map[string]string{
		".gitignore":                    "ui/node_modules/*\n!ui/node_modules/.keep\n",
		".golangci.yml":                 "version: \"2\"\nlinters:\n  exclusions:\n    paths:\n      - ui/node_modules\n",
		"ui/node_modules/vendor/pkg.go": "package vendor\n",
		"pkg/live.go":                   "package pkg\n",
	})
	if issues := (orphanedConfigPathRule{}).Run(LintContext{Root: dir}); len(issues) != 0 {
		t.Fatalf("existing ignored path flagged as orphaned: %+v", issues)
	}
}

// TestOrphanedConfigPath_AbsentPathOutsideGitIsOrphan pins the fallback: with
// no git repository there are no ignore rules to consult, so a missing literal
// path is stale as before.
func TestOrphanedConfigPath_AbsentPathOutsideGitIsOrphan(t *testing.T) {
	dir := writeLivenessFixture(t, map[string]string{
		".golangci.yml": "version: \"2\"\nlinters:\n  exclusions:\n    paths:\n      - ui/node_modules\n",
		"pkg/live.go":   "package pkg\n",
	})
	if issues := (orphanedConfigPathRule{}).Run(LintContext{Root: dir}); len(issues) != 1 {
		t.Fatalf("got %d findings outside git, want 1: %+v", len(issues), issues)
	}
}

func TestOrphanedConfigPath_GitignoredBaselineEntryIsNotOrphan(t *testing.T) {
	requireGit(t)
	dir := writeGitLivenessFixture(t, map[string]string{
		".gitignore":        "gen/\n",
		defaultBaselinePath: `{"version":1,"issues":[{"fingerprint":"a","file":"gen/out.go","rule":"complexity","count":1}]}`,
		"pkg/live.go":       "package pkg\n",
	})
	if issues := (orphanedConfigPathRule{}).Run(LintContext{Root: dir}); len(issues) != 0 {
		t.Fatalf("baseline entry for gitignored file flagged as orphaned: %+v", issues)
	}
}

// writeGitLivenessFixture is writeLivenessFixture inside an initialized git
// repository, so the ignore rules the fixture writes are in force.
func writeGitLivenessFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := writeLivenessFixture(t, files)
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	return dir
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
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
