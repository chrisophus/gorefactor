package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/chrisophus/gorefactor/analyzer"
	"github.com/chrisophus/gorefactor/config"
)

// lintFixOptions groups the flags controlling the autofix pass; embedded in
// lintOptions so field access stays flat (opts.fix, opts.verify, ...).
type lintFixOptions struct {
	fix        bool
	verify     bool
	probeFixes bool   // --probe-fixes: apply+gate+restore, journaling outcomes; tree unchanged
	fixLevel   string // gate each autofix (build+test) and revert it on failure
}

type lintOptions struct {
	root    string
	maxSize int
	maxSet  bool
	lintFixOptions
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

	// Ratchet mode (item 2): compare against / write a committed baseline so
	// only new-or-worsened issues fail. baseline and writeBaseline are the two
	// modes; baselineFile overrides the default committed path.
	baseline           bool
	writeBaseline      bool
	baselineFile       string
	baselineRatchetRef string
	noBaseline         bool

	// Hidden profiling flags (not advertised in help).
	cpuProfile   string // --cpuprofile <path>: write a CPU profile of the rule phase
	profileRules bool   // --profile-rules: print per-rule timing to stderr
}

func parseLintOptions(args []string) (lintOptions, error) {
	opts := lintOptions{
		root:           ".",
		lintFixOptions: lintFixOptions{fixLevel: fixLevelSafe},
		maxSize:        defaultSplitMaxLines,
		failOn:         "error",
		onlyRules:      make(map[string]bool),
		skipRules:      make(map[string]bool),
	}
	for i := 0; i < len(args); i++ {
		n, ok, err := opts.parseFlagAt(args, i)

		if err != nil {
			return opts, err
		}
		if ok {
			i += n
			continue
		}
		if strings.HasPrefix(args[i], "--") {
			return opts, fmt.Errorf("unknown lint flag: %s", args[i])
		}
		opts.root = args[i]
	}
	if opts.fixLevel == fixLevelAggressive && (!opts.fix || !opts.verify) && !opts.probeFixes {
		return opts, fmt.Errorf("--fix-level aggressive requires --fix --verify (or --probe-fixes): every aggressive fix must be build+test gated and revertible")
	}
	if opts.probeFixes && opts.fix {
		return opts, fmt.Errorf("--probe-fixes and --fix are mutually exclusive: probe is a sensor run that always restores the tree")
	}
	if opts.baseline && opts.writeBaseline {
		return opts, fmt.Errorf("--baseline and --write-baseline are mutually exclusive (compare vs record)")
	}
	if opts.noBaseline && opts.baseline {
		return opts, fmt.Errorf("--no-baseline and --baseline are mutually exclusive")
	}
	if err := opts.loadConfig(); err != nil {
		return opts, err
	}
	return opts, nil

}

func (opts *lintOptions) parseFlagAt(args []string, i int) (int, bool, error) {
	for _, group := range []func([]string, int) (int, bool, error){
		opts.parseBaselineFlags,
		opts.parseFixFlags,
		opts.parseOutputFlags,
		opts.parseConfigFlags,
	} {
		if n, ok, err := group(args, i); ok || err != nil {
			return n, ok, err
		}
	}
	return 0, false, nil

}

func (opts *lintOptions) parseBaselineFlags(args []string, i int) (int, bool, error) {
	switch args[i] {
	case "--baseline":
		opts.baseline = true
	case "--no-baseline":
		opts.noBaseline = true
	case "--baseline-ratchet":
		if i+1 >= len(args) {
			return 0, true, fmt.Errorf("--baseline-ratchet requires a git ref")
		}
		opts.baselineRatchetRef = args[i+1]
		return 1, true, nil
	case "--write-baseline":
		opts.writeBaseline = true
	case "--baseline-file":
		if i+1 >= len(args) {
			return 0, true, fmt.Errorf("--baseline-file requires a path")
		}
		opts.baselineFile = args[i+1]
		return 1, true, nil
	default:
		return 0, false, nil
	}
	return 0, true, nil
}

func (opts *lintOptions) parseFixFlags(args []string, i int) (int, bool, error) {
	switch args[i] {
	case "--fix":
		opts.fix = true
	case "--verify":
		opts.verify = true
	case "--probe-fixes":
		opts.probeFixes = true
	case "--fix-level":
		if i+1 >= len(args) {
			return 0, true, fmt.Errorf("--fix-level requires safe or aggressive")
		}
		switch args[i+1] {
		case fixLevelSafe, fixLevelAggressive:
			opts.fixLevel = args[i+1]
		default:
			return 0, true, fmt.Errorf("--fix-level must be safe or aggressive")
		}
		return 1, true, nil
	case "--rule":
		if i+1 >= len(args) {
			return 0, true, fmt.Errorf("--rule requires a value")
		}
		opts.onlyRules[args[i+1]] = true
		return 1, true, nil
	case "--skip-rule":
		if i+1 >= len(args) {
			return 0, true, fmt.Errorf("--skip-rule requires a value")
		}
		opts.skipRules[args[i+1]] = true
		return 1, true, nil
	default:
		return 0, false, nil
	}
	return 0, true, nil
}

func (opts *lintOptions) parseOutputFlags(args []string, i int) (int, bool, error) {
	switch args[i] {
	case "--json":
		opts.jsonOut = true
	case "--quiet":
		opts.quiet = true
	case "--fail-only":
		opts.failOnly = true
	case "--info":
		opts.info = true
	case "--verbose":
		opts.verbose = true
		opts.info = true
	case "--cpuprofile":
		if i+1 >= len(args) {
			return 0, true, fmt.Errorf("--cpuprofile requires a path")
		}
		opts.cpuProfile = args[i+1]
		return 1, true, nil
	case "--profile-rules":
		opts.profileRules = true
	default:
		return 0, false, nil
	}
	return 0, true, nil
}

func (opts *lintOptions) parseConfigFlags(args []string, i int) (int, bool, error) {
	switch args[i] {
	case "--config":
		if i+1 >= len(args) {
			return 0, true, fmt.Errorf("--config requires a value")
		}
		opts.configPath = args[i+1]
	case "--profile":
		if i+1 >= len(args) {
			return 0, true, fmt.Errorf("--profile requires a value")
		}
		opts.profile = args[i+1]
	case "--max":
		if i+1 >= len(args) {
			return 0, true, fmt.Errorf("--max requires a value")
		}
		var n int
		if _, err := fmt.Sscanf(args[i+1], "%d", &n); err != nil || n <= 0 {
			return 0, true, fmt.Errorf("--max requires a positive integer")
		}
		opts.maxSize = n
		opts.maxSet = true
	case "--fail-on":
		if i+1 >= len(args) {
			return 0, true, fmt.Errorf("--fail-on requires error or warning")
		}
		switch args[i+1] {
		case "error", "warning":
			opts.failOn = args[i+1]
		default:
			return 0, true, fmt.Errorf("--fail-on must be error or warning")
		}
	default:
		return 0, false, nil
	}
	return 1, true, nil
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
	return cfg.ValidateKnownRules(knownLintRuleNames())
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
		if issueFailsAt(iss, failOn) {
			return true
		}
	}
	return false
}
func (opts lintOptions) lintContext(files []string) LintContext {
	root := opts.root
	if fi, err := os.Stat(root); err == nil && !fi.IsDir() {
		// A single-file target must behave like its containing package dir:
		// Root seeds the verify gate's working directory, the outcome journal,
		// and the coverage-profile lookup, none of which work on a file path
		// (exec.Command with a file as Dir fails, so --fix --verify would
		// revert every fix it applied).
		root = filepath.Dir(root)
	}
	ctx := LintContext{
		Root:     root,
		FixLevel: opts.fixLevel,
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
