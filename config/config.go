package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/chrisophus/gorefactor/analyzer"
	"gopkg.in/yaml.v3"
)

const (
	defaultFileLengthSource = 500
	defaultFileLengthTest   = 1000
	defaultCouplingFanIn    = 8
	defaultCouplingFanOut   = 10
	defaultBaselineFile     = ".gorefactor-lint-baseline.json"
)

// Walk holds directory and file skip policy from YAML.
type Walk struct {
	SkipGeneratedGo bool     `yaml:"skip_generated_go"`
	SkipDirSegments []string `yaml:"skip_dir_segments"`
	SkipFiles       []string `yaml:"skip_files"`
}

// Limits holds file-size thresholds from YAML.
type Limits struct {
	FileLengthSource int `yaml:"file_length_source"`
	FileLengthTest   int `yaml:"file_length_test"`
}

// LintThresholds holds per-rule numeric thresholds from YAML.
type LintThresholds struct {
	FanIn  int `yaml:"fan_in"`
	FanOut int `yaml:"fan_out"`
}

// Lint holds lint-specific tuning from YAML.
type Lint struct {
	// DuplicateIgnore lists normalized-code substring patterns excluded from
	// duplicate-block detection, in addition to the built-in error idioms
	// (improvement plan item 6c). e.g. "if err != nil", "t.Fatal".
	DuplicateIgnore  []string                  `yaml:"duplicate-ignore"`
	ExcludeTestFiles []string                  `yaml:"exclude_test_files"`
	ExcludePackages  map[string][]string       `yaml:"exclude_packages"`
	Thresholds       map[string]LintThresholds `yaml:"thresholds"`
}

// TrackedArtifact holds allowlists for the tracked-artifact rule.
type TrackedArtifact struct {
	AllowExtensions   []string `yaml:"allow_extensions"`
	AllowPathPrefixes []string `yaml:"allow_path_prefixes"`
}

// Baseline holds committed baseline ratchet settings from YAML.
type Baseline struct {
	Enabled bool   `yaml:"enabled"`
	File    string `yaml:"file"`
}

// File is the parsed gorefactor lint configuration file.
type File struct {
	Walk            Walk             `yaml:"walk"`
	Limits          Limits           `yaml:"limits"`
	Lint            Lint             `yaml:"lint"`
	TrackedArtifact TrackedArtifact  `yaml:"tracked_artifact"`
	Baseline        Baseline         `yaml:"baseline"`
	Rules           map[string]Tier  `yaml:"rules"`
	Profiles        map[string]Rules `yaml:"profiles"`
	path            string
	hasRules        bool
	allowExtensions map[string]struct{}
	allowPrefixes   []string
}

// Path returns the loaded config file path, or empty when Load returned nil without error.
func (f *File) Path() string {
	if f == nil {
		return ""
	}
	return f.path
}

// HasRules reports whether the config file defined a rules section.
func (f *File) HasRules() bool {
	return f != nil && f.hasRules
}

// WalkOptions maps walk YAML to analyzer.WalkOptions.
func (f *File) WalkOptions() analyzer.WalkOptions {
	if f == nil {
		return analyzer.DefaultWalkOptions()
	}
	skipGen := f.Walk.SkipGeneratedGo
	if !skipGen && len(f.Walk.SkipDirSegments) == 0 && len(f.Walk.SkipFiles) == 0 {
		skipGen = true
	} else if len(f.Walk.SkipDirSegments) > 0 || len(f.Walk.SkipFiles) > 0 {
		// When walk skips are configured, default generated suffix skip to true unless explicitly false.
		if !f.Walk.SkipGeneratedGo {
			skipGen = true
		}
	}
	return analyzer.WalkOptions{
		SkipGeneratedGo:      skipGen,
		ExtraSkipDirSegments: append([]string(nil), f.Walk.SkipDirSegments...),
		SkipFilePaths:        append([]string(nil), f.Walk.SkipFiles...),
	}
}

// FileLengthLimits returns source and test file line limits.
func (f *File) FileLengthLimits() (source, test int) {
	if f == nil {
		return defaultFileLengthSource, defaultFileLengthTest
	}
	return f.Limits.FileLengthSource, f.Limits.FileLengthTest
}

// RuleTier returns the effective tier for a rule under profile (empty = ci/base rules only).
// ok=false means no config override — caller should use the rule's native default.
func (f *File) RuleTier(name, profile string) (tier Tier, ok bool) {
	if f == nil || !f.hasRules {
		return "", false
	}
	t, found := f.Rules[name]
	if !found {
		return TierOff, true
	}
	if profile != "" && f.Profiles != nil {
		if prof, exists := f.Profiles[profile]; exists {
			if pt, exists := prof[name]; exists {
				t = pt
			}
		}
	}
	return t, true
}

// TrackedArtifactAllowed reports whether a tracked git path is exempt from the rule.
func (f *File) TrackedArtifactAllowed(repoRelPath string) bool {
	if f == nil {
		return false
	}
	rel := NormalizeRepoRelativePath(repoRelPath)
	if ext := NormalizeExtension(filepath.Ext(rel)); ext != "" {
		if _, ok := f.allowExtensions[ext]; ok {
			return true
		}
	}
	for _, prefix := range f.allowPrefixes {
		if rel == prefix || strings.HasPrefix(rel, prefix) {
			return true
		}
	}
	return false
}

// CouplingThresholds returns fan-in and fan-out thresholds for high-coupling.
func (f *File) CouplingThresholds() (fanIn, fanOut int) {
	fanIn = defaultCouplingFanIn
	fanOut = defaultCouplingFanOut
	if f == nil {
		return fanIn, fanOut
	}
	if th, ok := f.Lint.Thresholds["high-coupling"]; ok {
		if th.FanIn > 0 {
			fanIn = th.FanIn
		}
		if th.FanOut > 0 {
			fanOut = th.FanOut
		}
	}
	return fanIn, fanOut
}

// ExcludeTestFileRuleSet returns rules that skip _test.go files.
func (f *File) ExcludeTestFileRuleSet() map[string]struct{} {
	out := make(map[string]struct{})
	if f == nil {
		return out
	}
	for _, rule := range f.Lint.ExcludeTestFiles {
		if rule = strings.TrimSpace(rule); rule != "" {
			out[rule] = struct{}{}
		}
	}
	return out
}

// ExcludedPackageSet returns excluded package paths for a rule.
func (f *File) ExcludedPackageSet(rule string) map[string]struct{} {
	out := make(map[string]struct{})
	if f == nil || f.Lint.ExcludePackages == nil {
		return out
	}
	for _, pkg := range f.Lint.ExcludePackages[rule] {
		if norm := NormalizeRepoRelativePath(pkg); norm != "" {
			out[norm] = struct{}{}
		}
	}
	return out
}

// BaselineEnabled reports whether YAML enables baseline comparison.
func (f *File) BaselineEnabled() bool {
	return f != nil && f.Baseline.Enabled
}

// BaselineFile returns the configured baseline path or the default filename.
func (f *File) BaselineFile() string {
	if f == nil || strings.TrimSpace(f.Baseline.File) == "" {
		return defaultBaselineFile
	}
	return strings.TrimSpace(f.Baseline.File)
}

// ValidateKnownRules rejects unknown rule names in policy sections.
func (f *File) ValidateKnownRules(known map[string]struct{}) error {
	if f == nil {
		return nil
	}
	check := func(rule, section string) error {
		if _, ok := known[rule]; !ok {
			return fmt.Errorf("config %s: unknown rule %q", section, rule)
		}
		return nil
	}
	for _, rule := range f.Lint.ExcludeTestFiles {
		if err := check(strings.TrimSpace(rule), "lint.exclude_test_files"); err != nil {
			return fmt.Errorf("validate lint policy: %w", err)
		}
	}
	for rule := range f.Lint.ExcludePackages {
		if err := check(rule, "lint.exclude_packages"); err != nil {
			return fmt.Errorf("validate lint policy: %w", err)
		}
	}
	for rule := range f.Lint.Thresholds {
		if err := check(rule, "lint.thresholds"); err != nil {
			return fmt.Errorf("validate lint policy: %w", err)
		}
	}
	for rule := range f.Rules {
		if err := check(rule, "rules"); err != nil {
			return fmt.Errorf("validate lint policy: %w", err)
		}
	}
	for profile, rules := range f.Profiles {
		for rule := range rules {
			if err := check(rule, "profiles."+profile); err != nil {
				return fmt.Errorf("validate lint policy: %w", err)
			}
		}
	}
	return nil
}

func (f *File) normalizeTrackedArtifact() {
	if f == nil {
		return
	}
	f.allowExtensions = make(map[string]struct{}, len(f.TrackedArtifact.AllowExtensions))
	for _, ext := range f.TrackedArtifact.AllowExtensions {
		if norm := NormalizeExtension(ext); norm != "" {
			f.allowExtensions[norm] = struct{}{}
		}
	}
	f.allowPrefixes = make([]string, 0, len(f.TrackedArtifact.AllowPathPrefixes))
	for _, prefix := range f.TrackedArtifact.AllowPathPrefixes {
		if norm := NormalizeRepoRelativePath(prefix); norm != "" {
			f.allowPrefixes = append(f.allowPrefixes, norm)
		}
	}
}

// Rules is a profile-specific rule tier map.
type Rules map[string]Tier

// Load reads config from path. When path is empty, discovers .gorefactor.yaml or gorefactor.yaml
// starting at startDir (typically the lint root) and walking up to the module root.
func Load(path, startDir string) (*File, error) {
	if path == "" {
		var err error
		path, err = discover(startDir)
		if err != nil {
			return nil, fmt.Errorf("discover: %w", err)
		}
		if path == "" {
			return nil, nil
		}
	}
	data, err := os.ReadFile(path) // #nosec G304 -- explicit config path from user or discovery
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	var f File
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	f.path = path
	f.hasRules = len(f.Rules) > 0
	normalizeFile(&f)
	return &f, nil
}

// NormalizeExtension lowercases and ensures a leading dot.
func NormalizeExtension(ext string) string {
	ext = strings.TrimSpace(ext)
	if ext == "" {
		return ""
	}
	ext = strings.ToLower(ext)
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return ext
}

// NormalizeRepoRelativePath normalizes a repo-relative path to slash form.
func NormalizeRepoRelativePath(path string) string {
	path = strings.TrimSpace(path)
	path = filepath.ToSlash(path)
	return strings.TrimPrefix(path, "./")
}

func normalizeFile(f *File) {
	if !f.Walk.SkipGeneratedGo && len(f.Walk.SkipDirSegments) == 0 && len(f.Walk.SkipFiles) == 0 {
		f.Walk.SkipGeneratedGo = true
	}
	if f.Limits.FileLengthSource <= 0 {
		f.Limits.FileLengthSource = defaultFileLengthSource
	}
	if f.Limits.FileLengthTest <= 0 {
		f.Limits.FileLengthTest = defaultFileLengthTest
	}
	for name, tier := range f.Rules {
		if err := validateTier(name, tier); err != nil {
			// Should not happen if yaml unmarshals to Tier; kept for profile merge.
			continue
		}
	}
	f.normalizeTrackedArtifact()
}

func validateTier(name string, tier Tier) error {
	if tier == "" {
		return fmt.Errorf("rule %q: empty tier", name)
	}
	_, err := ParseTier(string(tier))
	return err
}

func discover(startDir string) (string, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", err
	}
	names := []string{".gorefactor.yaml", "gorefactor.yaml"}
	for {
		for _, name := range names {
			candidate := filepath.Join(dir, name)
			if _, err := os.Stat(candidate); err == nil {
				return candidate, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", nil
		}
		dir = parent
	}
}
