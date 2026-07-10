package main

import (
	"fmt"
	"strings"

	"github.com/chrisophus/gorefactor/analyzer"
	"github.com/chrisophus/gorefactor/config"
)

type lintOptions struct {
	root       string
	maxSize    int
	maxSet     bool
	fix        bool
	verify     bool // --verify: gate each autofix (build+test) and revert it on failure
	jsonOut    bool
	quiet      bool
	failOnly   bool
	info       bool // --info: include [info] issues (default hides them)
	verbose    bool // --verbose: include everything, no collapsing
	configPath string
	profile    string
	cfg        *config.File
	onlyRules  map[string]bool
	skipRules  map[string]bool
	failOn     string // "error" | "warning"

	// Hidden profiling flags (not advertised in help).
	cpuProfile   string // --cpuprofile <path>: write a CPU profile of the rule phase
	profileRules bool   // --profile-rules: print per-rule timing to stderr
}

func parseLintOptions(args []string) (lintOptions, error) {
	opts := lintOptions{
		root:      ".",
		maxSize:   defaultSplitMaxLines,
		failOn:    "error",
		onlyRules: make(map[string]bool),
		skipRules: make(map[string]bool),
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--fix":
			opts.fix = true
		case a == "--verify":
			opts.verify = true
		case a == "--json":
			opts.jsonOut = true
		case a == "--quiet":
			opts.quiet = true
		case a == "--fail-only":
			opts.failOnly = true
		case a == "--info":
			opts.info = true
		case a == "--verbose":
			opts.verbose = true
			opts.info = true
		case a == "--cpuprofile":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--cpuprofile requires a path")
			}
			opts.cpuProfile = args[i+1]
			i++
		case a == "--profile-rules":
			opts.profileRules = true
		case a == "--config":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--config requires a value")
			}
			opts.configPath = args[i+1]
			i++
		case a == "--profile":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--profile requires a value")
			}
			opts.profile = args[i+1]
			i++
		case a == "--max":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--max requires a value")
			}
			var n int
			if _, err := fmt.Sscanf(args[i+1], "%d", &n); err != nil || n <= 0 {
				return opts, fmt.Errorf("--max requires a positive integer")
			}
			opts.maxSize = n
			opts.maxSet = true
			i++
		case a == "--rule":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--rule requires a value")
			}
			opts.onlyRules[args[i+1]] = true
			i++
		case a == "--skip-rule":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--skip-rule requires a value")
			}
			opts.skipRules[args[i+1]] = true
			i++
		case a == "--fail-on":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--fail-on requires error or warning")
			}
			switch args[i+1] {
			case "error", "warning":
				opts.failOn = args[i+1]
			default:
				return opts, fmt.Errorf("--fail-on must be error or warning")
			}
			i++
		case strings.HasPrefix(a, "--"):
			return opts, fmt.Errorf("unknown lint flag: %s", a)
		default:
			opts.root = a
		}
	}
	if err := opts.loadConfig(); err != nil {
		return opts, err
	}
	return opts, nil
}

func (opts *lintOptions) loadConfig() error {
	cfg, err := config.Load(opts.configPath, opts.root)
	if err != nil {
		return err
	}
	opts.cfg = cfg
	if cfg == nil {
		return nil
	}
	if !opts.maxSet {
		src, _ := cfg.FileLengthLimits()
		opts.maxSize = src
	}
	// Item 6c: feed configured duplicate-ignore patterns to the analyzer.
	analyzer.DuplicateIgnorePatterns = cfg.Lint.DuplicateIgnore
	return nil
}

func filterLintRules(all []LintRule, opts lintOptions) []LintRule {
	var out []LintRule
	for _, r := range all {
		name := r.Name()
		if len(opts.onlyRules) > 0 && !opts.onlyRules[name] {
			continue
		}
		if opts.skipRules[name] {
			continue
		}
		if opts.cfg != nil && opts.cfg.HasRules() {
			tier, ok := opts.cfg.RuleTier(name, opts.profile)
			if ok && tier == config.TierOff {
				continue
			}
		}
		out = append(out, r)
	}
	return out
}

func applyConfigSeverity(issues []lintIssue, opts lintOptions) []lintIssue {
	if opts.cfg == nil || !opts.cfg.HasRules() {
		return issues
	}
	out := make([]lintIssue, len(issues))
	for i, iss := range issues {
		out[i] = iss
		if tier, ok := opts.cfg.RuleTier(iss.Rule, opts.profile); ok && tier != config.TierOff {
			out[i].Severity = string(tier)
		}
	}
	return out
}

func lintShouldFail(issues []lintIssue, failOn string) bool {
	for _, iss := range issues {
		if failOn == "warning" {
			return true
		}
		if iss.Severity == "error" {
			return true
		}
	}
	return false
}
func (opts lintOptions) lintContext(files []string) LintContext {
	ctx := LintContext{
		Root:     opts.root,
		Files:    files,
		MaxSize:  opts.maxSize,
		WalkOpts: analyzer.DefaultWalkOptions(),
		Config:   opts.cfg,
		Profile:  opts.profile,
	}
	if opts.cfg != nil {
		ctx.WalkOpts = opts.cfg.WalkOptions()
		_, test := opts.cfg.FileLengthLimits()
		ctx.MaxSizeTest = test
	}
	return ctx
}
