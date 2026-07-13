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
	shouldFail := lintShouldFail(issues, opts.failOn)
	outputIssues := issues
	if opts.failOnly {
		outputIssues = failingIssues(issues, opts.failOn)
	}

	if opts.jsonOut {
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
	extractBlockL72(displayIssues, opts, shouldFail, outputIssues, issues, ctx, rules)
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

func extractBlockL72(displayIssues []lintIssue, opts lintOptions, shouldFail bool, outputIssues []lintIssue, issues []lintIssue, ctx LintContext, rules []LintRule) {
	if len(displayIssues) > 0 && (!opts.quiet || shouldFail) {
		byRule := map[string]int{}
		for _, iss := range displayIssues {
			byRule[iss.Rule]++
			fmt.Printf("%s [%s] %s: %s", iss.File, iss.Severity, iss.Rule, iss.Message)
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
			applied, reverted, failed := applyAutoFixes(issues, ctx, rules, opts.verify)
			if opts.verify {
				fmt.Printf("\nAuto-fixes: %d applied, %d reverted (gate failed), %d failed to apply\n",
					applied, reverted, failed)
			} else {
				fmt.Printf("\nAuto-fixes: %d applied, %d failed\n", applied, failed)
			}
		}
	}
}
