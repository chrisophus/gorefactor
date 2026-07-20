package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeExtension(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"PNG":  ".png",
		".ico": ".ico",
		"tgz":  ".tgz",
		"":     "",
	}
	for in, want := range cases {
		if got := NormalizeExtension(in); got != want {
			t.Fatalf("NormalizeExtension(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTrackedArtifactAllowed(t *testing.T) {
	t.Parallel()
	f := &File{
		TrackedArtifact: TrackedArtifact{
			AllowExtensions:   []string{".png", ".tgz"},
			AllowPathPrefixes: []string{"docs/assets/", "ui/vendor/"},
		},
	}
	f.normalizeTrackedArtifact()

	cases := []struct {
		path string
		want bool
	}{
		{"docs/assets/logo.png", true},
		{"ui/vendor/pkg.tgz", true},
		{"bin/tool", false},
		{"pkg/testdata/fixture.bin", false},
	}
	for _, tc := range cases {
		if got := f.TrackedArtifactAllowed(tc.path); got != tc.want {
			t.Fatalf("TrackedArtifactAllowed(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestCouplingThresholds_Defaults(t *testing.T) {
	t.Parallel()
	fanIn, fanOut := (&File{}).CouplingThresholds()
	if fanIn != defaultCouplingFanIn || fanOut != defaultCouplingFanOut {
		t.Fatalf("defaults = %d/%d, want %d/%d", fanIn, fanOut, defaultCouplingFanIn, defaultCouplingFanOut)
	}
}

func TestCouplingThresholds_FromYAML(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, ".gorefactor.yaml")
	const yamlDoc = `
lint:
  thresholds:
    high-coupling:
      fan_in: 12
      fan_out: 15
rules:
  file-size: error
`
	if err := os.WriteFile(path, []byte(yamlDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := Load(path, dir)
	if err != nil {
		t.Fatal(err)
	}
	fanIn, fanOut := f.CouplingThresholds()
	if fanIn != 12 || fanOut != 15 {
		t.Fatalf("thresholds = %d/%d, want 12/15", fanIn, fanOut)
	}
}

func TestValidateKnownRules_RejectsUnknown(t *testing.T) {
	t.Parallel()
	known := map[string]struct{}{"file-size": {}, "high-coupling": {}}
	f := &File{
		Rules: map[string]Tier{"file-size": TierError},
		Lint: Lint{
			ExcludeTestFiles: []string{"unknown-rule"},
		},
	}
	if err := f.ValidateKnownRules(known); err == nil {
		t.Fatal("expected error for unknown exclude_test_files rule")
	}
}

func TestExcludeTestFileRuleSet(t *testing.T) {
	t.Parallel()
	f := &File{Lint: Lint{ExcludeTestFiles: []string{"error-not-wrapped", "error-not-wrapped"}}}
	set := f.ExcludeTestFileRuleSet()
	if _, ok := set["error-not-wrapped"]; !ok || len(set) != 1 {
		t.Fatalf("set = %v", set)
	}
}

func TestExcludedPackageSet(t *testing.T) {
	t.Parallel()
	f := &File{Lint: Lint{ExcludePackages: map[string][]string{
		"high-coupling": {"internal/domain", "internal/wire"},
	}}}
	set := f.ExcludedPackageSet("high-coupling")
	if len(set) != 2 {
		t.Fatalf("set = %v", set)
	}
	if _, ok := set["internal/domain"]; !ok {
		t.Fatalf("missing internal/domain in %v", set)
	}
	if _, ok := set["internal/wire"]; !ok {
		t.Fatalf("missing internal/wire in %v", set)
	}
}

func TestBaselineConfig_DefaultFile(t *testing.T) {
	t.Parallel()
	f := &File{Baseline: Baseline{Enabled: true}}
	if got := f.BaselineFile(); got != defaultBaselineFile {
		t.Fatalf("BaselineFile() = %q, want %q", got, defaultBaselineFile)
	}
}

func TestBaselineConfig_CustomFile(t *testing.T) {
	t.Parallel()
	f := &File{Baseline: Baseline{Enabled: true, File: "custom-baseline.json"}}
	if got := f.BaselineFile(); got != "custom-baseline.json" {
		t.Fatalf("BaselineFile() = %q", got)
	}
}
