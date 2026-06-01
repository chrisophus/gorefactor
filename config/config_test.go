package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_WalkAndLimits(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, ".gorefactor.yaml")
	const yamlDoc = `
walk:
  skip_dir_segments:
    - api/gen
  skip_files:
    - internal/data/model.go
limits:
  file_length_source: 400
  file_length_test: 800
rules:
  file-size: error
  complexity: off
profiles:
  deep:
    dead-code: info
`
	if err := os.WriteFile(path, []byte(yamlDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := Load(path, dir)
	if err != nil {
		t.Fatal(err)
	}
	opts := f.WalkOptions()
	if len(opts.ExtraSkipDirSegments) != 1 || opts.ExtraSkipDirSegments[0] != "api/gen" {
		t.Fatalf("skip dirs = %v", opts.ExtraSkipDirSegments)
	}
	if len(opts.SkipFilePaths) != 1 {
		t.Fatalf("skip files = %v", opts.SkipFilePaths)
	}
	src, test := f.FileLengthLimits()
	if src != 400 || test != 800 {
		t.Fatalf("limits = %d/%d", src, test)
	}
}

func TestRuleTier_ProfileOverlay(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, ".gorefactor.yaml")
	const yamlDoc = `
rules:
  duplicate-block: off
  dead-code: off
profiles:
  deep:
    duplicate-block: warning
    dead-code: info
`
	if err := os.WriteFile(path, []byte(yamlDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := Load(path, dir)
	if err != nil {
		t.Fatal(err)
	}
	tier, ok := f.RuleTier("duplicate-block", "")
	if !ok || tier != TierOff {
		t.Fatalf("ci duplicate-block = %q ok=%v", tier, ok)
	}
	tier, ok = f.RuleTier("duplicate-block", "deep")
	if !ok || tier != TierWarning {
		t.Fatalf("deep duplicate-block = %q ok=%v", tier, ok)
	}
	tier, ok = f.RuleTier("dead-code", "deep")
	if !ok || tier != TierInfo {
		t.Fatalf("deep dead-code = %q ok=%v", tier, ok)
	}
}

func TestRuleTier_UnlistedRuleOff(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, ".gorefactor.yaml")
	if err := os.WriteFile(path, []byte("rules:\n  file-size: error\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := Load(path, dir)
	if err != nil {
		t.Fatal(err)
	}
	tier, ok := f.RuleTier("complexity", "")
	if !ok || tier != TierOff {
		t.Fatalf("unlisted = %q ok=%v want off", tier, ok)
	}
}

func TestDiscover_FindsGorefactorYaml(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sub := filepath.Join(root, "pkg")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(root, ".gorefactor.yaml")
	if err := os.WriteFile(cfgPath, []byte("rules:\n  file-size: error\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := discover(sub)
	if err != nil {
		t.Fatal(err)
	}
	if got != cfgPath {
		t.Fatalf("discover = %q want %q", got, cfgPath)
	}
}
