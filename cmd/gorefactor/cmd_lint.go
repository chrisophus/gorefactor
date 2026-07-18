package main

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
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
		return cmp.Or(
			strings.Compare(a.File, b.File),
			strings.Compare(a.Rule, b.Rule),
			strings.Compare(a.Severity, b.Severity),
			strings.Compare(a.Message, b.Message),
		) < 0
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

const (
	fixLevelSafe       = "safe"
	fixLevelAggressive = "aggressive"
)

// defaultAutoFixBatchSize bounds how many fixes are applied together before
// one verify-gate run under --verify. A batch's build+test cost is paid once
// for the whole group when the gate stays green (the common case, since most
// fixes are good); bisectAutoFixBatch binary-searches a red batch to isolate
// the culprit(s) in O(log K) extra gate runs instead of gating every fix
// one at a time.
const defaultAutoFixBatchSize = 8

// applyAutoFixes runs every issue's registered autofix. With verify set,
// fixes are applied in batches of up to defaultAutoFixBatchSize and gated
// together — see bisectAutoFixBatch for how a red batch is resolved down to
// the exact fix(es) that broke it. Without verify it is the original
// apply-and-hope behavior, ungated and one at a time.
func applyAutoFixes(issues []lintIssue, ctx LintContext, rules []LintRule, verify bool) (applied, reverted, failed int) {
	rulesByName := make(map[string]LintRule, len(rules))
	for _, r := range rules {
		rulesByName[r.Name()] = r
	}
	fixerByRule := make(map[string]FixableRule, len(rules))
	var fixable []lintIssue
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
		fixerByRule[iss.Rule] = fixer
		fixable = append(fixable, iss)
	}

	if !verify {
		for _, iss := range fixable {
			if err := fixerByRule[iss.Rule].AutoFix(iss, ctx); err != nil {
				fmt.Fprintf(os.Stderr, "fix failed for %s: %v\n", iss.File, err)
				failed++
				continue
			}
			applied++
		}
		return
	}

	for i := 0; i < len(fixable); i += defaultAutoFixBatchSize {
		end := i + defaultAutoFixBatchSize
		if end > len(fixable) {
			end = len(fixable)
		}
		a, r, f := bisectAutoFixBatch(fixable[i:end], ctx, fixerByRule)
		applied += a
		reverted += r
		failed += f
	}
	return

}

// bisectAutoFixBatch applies every issue in pending, snapshotting each distinct package directory
// involved before touching it, then runs one verify-gate call for the whole group. A green gate
// keeps every fix that applied cleanly — one gate run for the whole group. A red gate means at
// least one fix in the group is bad: every touched directory is restored to its pre-group snapshot,
// the group is split in half, and each half is retried independently (binary search) — a clean
// half resolves in a single gate call, a half that's still red keeps splitting until the exact
// culprit(s) are isolated and reverted on their own.
func bisectAutoFixBatch(pending []lintIssue, ctx LintContext, fixerByRule map[string]FixableRule) (applied, reverted, failed int) {
	dirs := make([]string, 0, len(pending))
	seen := map[string]bool{}
	for _, iss := range pending {
		d := filepath.Dir(iss.File)
		if !seen[d] {
			seen[d] = true
			dirs = append(dirs, d)
		}
	}
	snaps := make(map[string]dirSnapshot, len(dirs))
	for _, d := range dirs {
		if s, serr := snapshotGoDir(d); serr != nil {
			fmt.Fprintf(os.Stderr, "verify: cannot snapshot %s: %v (applying unguarded)\n", d, serr)
		} else {
			snaps[d] = s
		}
	}

	var succeeded []lintIssue
	for _, iss := range pending {
		if err := fixerByRule[iss.Rule].AutoFix(iss, ctx); err != nil {
			fmt.Fprintf(os.Stderr, "fix failed for %s: %v\n", iss.File, err)
			failed++
			continue
		}
		succeeded = append(succeeded, iss)
	}
	if len(succeeded) == 0 {
		return 0, 0, failed
	}

	gerr := verifyGateFn(ctx.Root)
	if gerr == nil {
		return len(succeeded), 0, failed
	}

	for _, d := range dirs {
		if s, ok := snaps[d]; ok {
			if rerr := s.restore(); rerr != nil {
				fmt.Fprintf(os.Stderr, "verify: revert of %s failed: %v\n", d, rerr)
			}
		}
	}

	if len(succeeded) == 1 {
		fmt.Fprintf(os.Stderr, "reverted %s [%s]: gate failed after fix\n%v\n", succeeded[0].File, succeeded[0].Rule, gerr)
		return 0, 1, failed
	}

	mid := len(succeeded) / 2
	a1, r1, f1 := bisectAutoFixBatch(succeeded[:mid], ctx, fixerByRule)
	a2, r2, f2 := bisectAutoFixBatch(succeeded[mid:], ctx, fixerByRule)
	return a1 + a2, r1 + r2, failed + f1 + f2
}

type LintContext struct {
	Root        string
	Files       []string
	MaxSize     int
	MaxSizeTest int
	WalkOpts    analyzer.WalkOptions
	Config      *config.File
	Profile     string
	FixLevel    string
}

func (c LintContext) AggressiveFix() bool { return c.FixLevel == fixLevelAggressive }

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

func isTestFile(path string) bool {
	return strings.HasSuffix(path, "_test.go")
}

const longFunctionTestFactor = 2

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
	rules = append(rules, funcorderRules()...)
	rules = append(rules, smellRules()...)
	rules = append(rules,
		duplicateRule{},
		deadCodeRule{},
		untestedPackageRule{},
		untestedFunctionRule{},
		vacuousTestRule{},
		sleepInTestRule{},
		fatalInLibraryRule{},
		stringConcatInLoopRule{},
		linearSearchInLoopRule{},
		unstoppedTickerRule{},
		nakedGoroutineRule{},
		passThroughParamRule{},
		regexpHoistRule{},
		lowAdherenceRule{},
	)
	return rules
}
