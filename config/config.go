package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/chrisophus/gorefactor/analyzer"
	"gopkg.in/yaml.v3"
)

const (
	defaultFileLengthSource = 500
	defaultFileLengthTest   = 1000
)

// Walk holds directory and file skip policy from YAML.
type Walk struct {
	SkipGeneratedGo  bool     `yaml:"skip_generated_go"`
	SkipDirSegments  []string `yaml:"skip_dir_segments"`
	SkipFiles        []string `yaml:"skip_files"`
}

// Limits holds file-size thresholds from YAML.
type Limits struct {
	FileLengthSource int `yaml:"file_length_source"`
	FileLengthTest   int `yaml:"file_length_test"`
}

// File is the parsed gorefactor lint configuration file.
type File struct {
	Walk     Walk              `yaml:"walk"`
	Limits   Limits            `yaml:"limits"`
	Rules    map[string]Tier   `yaml:"rules"`
	Profiles map[string]Rules  `yaml:"profiles"`
	path     string
	hasRules bool
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
			return nil, err
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
}

func validateTier(name string, tier Tier) error {
	if tier == "" {
		return fmt.Errorf("rule %q: empty tier", name)
	}
	_, err := ParseTier(string(tier))
	return err
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
