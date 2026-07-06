package main

import (
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

func failingIssueCount(issues []lintIssue, failOn string) int {
	if failOn == "warning" {
		return len(issues)
	}
	count := 0
	for _, iss := range issues {
		if iss.Severity == "error" {
			count++
		}
	}
	return count
}

func failingIssues(issues []lintIssue, failOn string) []lintIssue {
	filtered := make([]lintIssue, 0, len(issues))
	for _, iss := range issues {
		if failOn == "warning" || iss.Severity == "error" {
			filtered = append(filtered, iss)
		}
	}
	return filtered
}

func collectGoFiles(root string, walk analyzer.WalkOptions) ([]string, error) {
	return analyzer.WalkGoFiles(root, walk)
}

// filterDisplayIssues shapes issues for human (non-JSON) output per improvement
// plan item 5. By default [info] issues (blast-radius, untested-*) are hidden so
// actionable warnings aren't buried. --info restores them but collapses repeated
// high-blast-radius entries per file into a single summary line. --verbose shows
// everything verbatim. JSON output is never filtered — machine consumers get all.
func filterDisplayIssues(issues []lintIssue, opts lintOptions) []lintIssue {
	if opts.verbose {
		return issues
	}
	out := make([]lintIssue, 0, len(issues))
	blastByFile := map[string]int{}
	for _, iss := range issues {
		if iss.Severity == "info" {
			if !opts.info {
				continue
			}
			if iss.Rule == "high-blast-radius" {
				blastByFile[iss.File]++
				continue
			}
		}
		out = append(out, iss)
	}
	for file, n := range blastByFile {
		msg := fmt.Sprintf("%d high-blast-radius function(s) — run `gorefactor blast-radius <func>` for details", n)
		out = append(out, lintIssue{File: file, Rule: "high-blast-radius", Severity: "info", Message: msg})
	}
	sortLintIssues(out)
	return out
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
		longFunctionRule{},
		deepNestingRule{},
		errWrapRule{},
		couplingRule{},
		blastRadiusRule{},
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
		lowAdherenceRule{},
	)
	return rules
}
