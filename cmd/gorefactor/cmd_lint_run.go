package main

import (
	"encoding/json"
	"fmt"
	"os"
)

func lintCommand(args []string) error {
	opts, err := parseLintOptions(args)
	if err != nil {
		return err
	}
	if opts.verify && !opts.fix {
		fmt.Fprintln(os.Stderr, "note: --verify only applies with --fix; ignoring")
	}

	if opts.baselineRatchetRef != "" {
		// Pure file comparison — no lint run needed.
		return baselineRatchetCommand(opts.baselineFilePath(), opts.baselineRatchetRef)
	}

	ctx := opts.lintContext(nil)
	files, err := collectGoFiles(opts.root, ctx.WalkOpts)
	if err != nil {
		return err
	}
	ctx.Files = files

	rules := filterLintRules(defaultLintRules(), opts)
	issues := runLintRules(rules, ctx, opts)
	issues = filterIssuesByLintPolicy(issues, ctx)
	issues = applyConfigSeverity(issues, opts)
	sortLintIssues(issues)

	issues, done, err := lintApplyBaselineMode(issues, opts)
	if err != nil {
		return err
	}
	if done {
		return nil
	}

	shouldFail := lintShouldFail(issues, opts.failOn)
	outputIssues := issues
	if opts.failOnly {
		outputIssues = failingIssues(issues, opts.failOn)
	}

	if opts.jsonOut {
		return lintOutputJSON(outputIssues, issues, opts, shouldFail)
	}

	if len(issues) == 0 {
		if !opts.quiet {
			fmt.Println("No issues found.")
		}
		return nil
	}

	displayIssues := filterDisplayIssues(outputIssues, opts)
	if len(displayIssues) == 0 && !shouldFail {
		if !opts.quiet {
			fmt.Println("No issues found.")
		}
		return nil
	}
	lintPrintIssuesAndFix(displayIssues, opts, shouldFail, outputIssues, issues, ctx, rules)
	if shouldFail {
		return fmt.Errorf(
			"lint: %d issue(s) at or above %s severity (%d total issue(s))",
			failingIssueCount(issues, opts.failOn),
			opts.failOn,
			len(issues),
		)
	}
	return nil

}

// lintPrintIssuesAndFix renders the human-readable issue list with a per-rule summary and, with
// --fix, runs the autofix pass and prints its outcome.
func lintPrintIssuesAndFix(displayIssues []lintIssue, opts lintOptions, shouldFail bool, outputIssues []lintIssue, issues []lintIssue, ctx LintContext, rules []LintRule) {
	if len(displayIssues) > 0 && (!opts.quiet || shouldFail) {
		byRule := map[string]int{}
		for _, iss := range displayIssues {
			byRule[iss.Rule]++
			fmt.Printf("%s [%s] %s: %s", iss.File, iss.Severity, iss.Rule, iss.Message)
			if iss.Note != "" {
				fmt.Printf("  [%s]", iss.Note)
			}
			if iss.AutoFix != "" {
				fmt.Printf("  (autofix: %s)", iss.AutoFix)
			}
			fmt.Println()
		}
		fmt.Println()
		if hidden := len(outputIssues) - len(displayIssues); hidden > 0 && !opts.info {
			fmt.Printf("Summary: %d shown, %d [info] hidden (use --info to show)\n", len(displayIssues), hidden)
		} else {
			fmt.Printf("Summary: %d issue(s)\n", len(displayIssues))
		}
		for rule, n := range byRule {
			fmt.Printf("  %s: %d\n", rule, n)
		}

		if opts.fix {
			applied, reverted, failed := applyAutoFixes(issues, ctx, rules, opts.verify, false)
			if opts.verify {
				fmt.Printf("\nAuto-fixes: %d applied, %d reverted (gate failed), %d not applied (no target, or skipped as previously reverted)\n",
					applied, reverted, failed)
			} else {
				fmt.Printf("\nAuto-fixes: %d applied, %d failed\n", applied, failed)
			}
		}
		if opts.probeFixes {
			verified, wouldRevert, failed := applyAutoFixes(issues, ctx, rules, true, true)
			fmt.Printf("\nProbe: %d verified safe, %d would be reverted by the gate, %d not attempted (no target, or previously reverted) — outcomes journaled, tree unchanged\n",
				verified, wouldRevert, failed)
		}
	}
}

func lintApplyBaselineMode(issues []lintIssue, opts lintOptions) ([]lintIssue, bool, error) {
	if opts.writeBaseline {
		path := opts.baselineFilePath()
		if err := writeBaseline(path, issues); err != nil {
			return issues, false, err
		}
		if !opts.quiet {
			fmt.Printf("Wrote lint baseline: %d issue(s) recorded -> %s\n", len(issues), path)
		}
		return issues, true, nil
	}
	if opts.baselineCompareEnabled() {
		base, err := loadBaseline(opts.baselineFilePath())
		if err != nil {
			return issues, false, err
		}
		before := len(issues)
		issues = filterAgainstBaseline(issues, base)
		if !opts.quiet && !opts.jsonOut {
			fmt.Printf("Baseline: %d pre-existing issue(s) suppressed, %d new/worsened\n",
				before-len(issues), len(issues))
		}
	}
	return issues, false, nil
}

func (opts lintOptions) baselineCompareEnabled() bool {
	if opts.noBaseline {
		return false
	}
	if opts.baseline {
		return true
	}
	if opts.cfg != nil {
		return opts.cfg.BaselineEnabled()
	}
	return false
}

func lintOutputJSON(outputIssues, issues []lintIssue, opts lintOptions, shouldFail bool) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(map[string]interface{}{
		"issues": outputIssues,
		"summary": map[string]int{
			"total":   len(outputIssues),
			"failing": failingIssueCount(issues, opts.failOn),
		},
	}); err != nil {
		return err
	}
	if shouldFail {
		return fmt.Errorf(
			"lint: %d issue(s) at or above %s severity (%d total issue(s))",
			failingIssueCount(issues, opts.failOn),
			opts.failOn,
			len(issues),
		)
	}
	return nil
}
