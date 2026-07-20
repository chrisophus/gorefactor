package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBaselineCompareEnabled_ConfigAndFlags(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".gorefactor.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
baseline:
  enabled: true
rules:
  file-size: error
`), 0o644); err != nil {
		t.Fatal(err)
	}
	opts, err := parseLintOptions([]string{dir, "--config", cfgPath})
	if err != nil {
		t.Fatal(err)
	}
	if !opts.baselineCompareEnabled() {
		t.Fatal("expected config baseline enabled")
	}

	opts.noBaseline = true
	if opts.baselineCompareEnabled() {
		t.Fatal("expected --no-baseline to disable compare")
	}

	opts.noBaseline = false
	opts.baseline = true
	if !opts.baselineCompareEnabled() {
		t.Fatal("expected explicit --baseline to enable compare")
	}
}

func TestBaselineFilePath_ConfigOverride(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".gorefactor.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
baseline:
  enabled: true
  file: custom-baseline.json
rules:
  file-size: error
`), 0o644); err != nil {
		t.Fatal(err)
	}
	opts, err := parseLintOptions([]string{dir, "--config", cfgPath})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "custom-baseline.json")
	if got := opts.baselineFilePath(); got != want {
		t.Fatalf("baselineFilePath() = %q, want %q", got, want)
	}

	opts.baselineFile = "cli-baseline.json"
	want = filepath.Join(dir, "cli-baseline.json")
	if got := opts.baselineFilePath(); got != want {
		t.Fatalf("baselineFilePath() with CLI override = %q, want %q", got, want)
	}
}
