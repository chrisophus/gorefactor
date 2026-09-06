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

func TestRuleDisabled_SubtractiveAndAllowlistIndependent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, ".gorefactor.yaml")
	const yamlDoc = `
lint:
  disable:
    - high-blast-radius
    - untested-function
`
	if err := os.WriteFile(path, []byte(yamlDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := Load(path, dir)
	if err != nil {
		t.Fatal(err)
	}
	if !f.RuleDisabled("high-blast-radius") || !f.RuleDisabled("untested-function") {
		t.Fatal("listed rules must be disabled")
	}
	if f.RuleDisabled("file-size") {
		t.Fatal("unlisted rule must not be disabled")
	}
	// disable is subtractive: it must not turn on the rules allowlist, so an
	// unlisted rule keeps its native tier (RuleTier reports no override).
	if f.HasRules() {
		t.Fatal("disable alone must not enable the rules allowlist")
	}
	if _, ok := f.RuleTier("file-size", ""); ok {
		t.Fatal("disable must not force other rules off via the allowlist")
	}
}

func TestValidateKnownRules_RejectsUnknownDisable(t *testing.T) {
	t.Parallel()
	f := &File{Lint: Lint{Disable: []string{"not-a-real-rule"}}}
	err := f.ValidateKnownRules(map[string]struct{}{"file-size": {}})
	if err == nil {
		t.Fatal("expected error for unknown disabled rule")
	}
}
