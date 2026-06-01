package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/chrisophus/gorefactor/analyzer"
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

	files, err := collectGoFiles(opts.root)
	if err != nil {
		return err
	}

	rules := filterLintRules(defaultLintRules(), opts)
	ctx := LintContext{Root: opts.root, Files: files, MaxSize: opts.maxSize}
	var issues []lintIssue
	for _, rule := range rules {
		issues = append(issues, rule.Run(ctx)...)
	}

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
		fmt.Println("No issues found.")
		return nil
	}

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
	if lintShouldFail(issues, opts.failOn) {
		return fmt.Errorf("lint: %d issue(s) at or above %s severity", len(issues), opts.failOn)
	}
	return nil
}

func collectGoFiles(root string) ([]string, error) {
	return analyzer.WalkGoFiles(root, analyzer.DefaultWalkOptions())
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
