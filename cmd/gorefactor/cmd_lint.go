package main

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"runtime/pprof"
	"sort"
	"strings"
	"time"

	"github.com/chrisophus/gorefactor/analyzer"
	"github.com/chrisophus/gorefactor/config"
	"golang.org/x/sync/errgroup"
)

type lintIssue struct {
	File       string `json:"file"`
	Rule       string `json:"rule"`
	Severity   string `json:"severity"`
	Message    string `json:"message"`
	AutoFix    string `json:"autofix,omitempty"`
	AutoFixCmd string `json:"autofixCmd,omitempty"`
}

func lintCommand(args []string) error {
	opts, err := parseLintOptions(args)
	if err != nil {
		return err
	}

	ctx := opts.lintContext(nil)
	files, err := collectGoFiles(opts.root, ctx.WalkOpts)
	if err != nil {
		return err
	}
	ctx.Files = files

	rules := filterLintRules(defaultLintRules(), opts)
	issues := runLintRules(rules, ctx, opts)
	issues = applyConfigSeverity(issues, opts)
	sortLintIssues(issues)

	if opts.jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(map[string]interface{}{
			"issues": issues,
			"summary": map[string]int{
				"total": len(issues),
			},
		}); err != nil {
			return err
		}
		if lintShouldFail(issues, opts.failOn) {
			return fmt.Errorf("lint: %d issue(s) at or above %s severity", len(issues), opts.failOn)
		}
		return nil
	}

	if len(issues) == 0 {
		if !opts.quiet {
			fmt.Println("No issues found.")
		}
		return nil
	}

	shouldFail := lintShouldFail(issues, opts.failOn)
	if !opts.quiet || shouldFail {
		byRule := map[string]int{}
		for _, iss := range issues {
			byRule[iss.Rule]++
			fmt.Printf("%s [%s] %s: %s", iss.File, iss.Severity, iss.Rule, iss.Message)
			if iss.AutoFix != "" {
				fmt.Printf("  (autofix: %s)", iss.AutoFix)
			}
			fmt.Println()
		}
		fmt.Println()
		fmt.Printf("Summary: %d issue(s)\n", len(issues))
		for rule, n := range byRule {
			fmt.Printf("  %s: %d\n", rule, n)
		}

		if opts.fix {
			applied, failed := applyAutoFixes(issues, ctx, rules)
			fmt.Printf("\nAuto-fixes: %d applied, %d failed\n", applied, failed)
		}
	}
	if shouldFail {
		return fmt.Errorf("lint: %d issue(s) at or above %s severity", len(issues), opts.failOn)
	}
	return nil
}

func collectGoFiles(root string, walk analyzer.WalkOptions) ([]string, error) {
	return analyzer.WalkGoFiles(root, walk)
}

// sortLintIssues orders issues deterministically so output is stable regardless
// of rule execution order (rules run concurrently) or map-iteration order
// inside individual rules.
func sortLintIssues(issues []lintIssue) {
	sort.SliceStable(issues, func(i, j int) bool {
		a, b := issues[i], issues[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Rule != b.Rule {
			return a.Rule < b.Rule
		}
		if a.Severity != b.Severity {
			return a.Severity < b.Severity
		}
		return a.Message < b.Message
	})
}

// runLintRules executes the given rules and aggregates their issues. It honors
// the hidden --cpuprofile and --profile-rules options for performance work.
func runLintRules(rules []LintRule, ctx LintContext, opts lintOptions) []lintIssue {
	if opts.cpuProfile != "" {
		f, err := os.Create(opts.cpuProfile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "cpuprofile: %v\n", err)
		} else {
			defer f.Close()
			if err := pprof.StartCPUProfile(f); err != nil {
				fmt.Fprintf(os.Stderr, "cpuprofile: %v\n", err)
			} else {
				defer pprof.StopCPUProfile()
			}
		}
	}

	type ruleTiming struct {
		name string
		dur  time.Duration
	}
	timings := make([]ruleTiming, len(rules))
	wallStart := time.Now()

	// Rules are read-only and independent, so run them concurrently. Results are
	// written to an index-aligned slice to keep aggregation order deterministic
	// (identical to the previous sequential append order).
	results := make([][]lintIssue, len(rules))
	var g errgroup.Group
	g.SetLimit(runtime.GOMAXPROCS(0))
	for i, rule := range rules {
		g.Go(func() error {
			start := time.Now()
			results[i] = rule.Run(ctx)
			timings[i] = ruleTiming{rule.Name(), time.Since(start)}
			return nil
		})
	}
	_ = g.Wait()

	var issues []lintIssue
	for _, res := range results {
		issues = append(issues, res...)
	}

	if opts.profileRules {
		wall := time.Since(wallStart)
		sort.Slice(timings, func(i, j int) bool { return timings[i].dur > timings[j].dur })
		fmt.Fprintln(os.Stderr, "── lint rule timings ──")
		for _, t := range timings {
			fmt.Fprintf(os.Stderr, "  %-26s %8.1fms\n", t.name, float64(t.dur.Microseconds())/1000)
		}
		fmt.Fprintf(os.Stderr, "  %-26s %8.1fms\n", "TOTAL (wall)", float64(wall.Microseconds())/1000)
	}

	return issues
}

func applyAutoFixes(issues []lintIssue, ctx LintContext, rules []LintRule) (applied, failed int) {
	rulesByName := make(map[string]LintRule, len(rules))
	for _, r := range rules {
		rulesByName[r.Name()] = r
	}
	for _, iss := range issues {
		if iss.AutoFixCmd == "" {
			continue
		}
		rule, ok := rulesByName[iss.Rule]
		if !ok {
			continue
		}
		fixer, ok := rule.(FixableRule)
		if !ok {
			continue
		}
		if err := fixer.AutoFix(iss, ctx); err != nil {
			fmt.Fprintf(os.Stderr, "fix failed for %s: %v\n", iss.File, err)
			failed++
			continue
		}
		applied++
	}
	return
}

type LintContext struct {
	Root        string
	Files       []string
	MaxSize     int
	MaxSizeTest int
	WalkOpts    analyzer.WalkOptions
	Config      *config.File
	Profile     string
}

func effectiveMaxSizeForFile(file string, ctx LintContext) int {
	if strings.HasSuffix(file, "_test.go") {
		if ctx.MaxSizeTest > 0 {
			return ctx.MaxSizeTest
		}
		if ctx.MaxSize > 0 {
			return ctx.MaxSize * 2
		}
		return defaultTestFileMaxLines
	}
	if ctx.MaxSize > 0 {
		return ctx.MaxSize
	}
	return defaultSplitMaxLines
}

type LintRule interface {
	Name() string
	Run(ctx LintContext) []lintIssue
}

type FixableRule interface {
	LintRule
	AutoFix(issue lintIssue, ctx LintContext) error
}

func defaultLintRules() []LintRule {
	rules := []LintRule{
		fileSizeRule{},
		extractableRule{},
		complexityRule{},
		errWrapRule{},
		couplingRule{},
		prematureAbstractionRule{},
	}
	rules = append(rules, logPropagationRules()...)
	rules = append(rules, smellRules()...)
	rules = append(rules,
		duplicateRule{},
		deadCodeRule{},
		untestedPackageRule{},
		untestedFunctionRule{},
		golangciLintRule{},
		archLintRule{},
	)
	return rules
}
