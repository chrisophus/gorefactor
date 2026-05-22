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
	root := "."
	maxSize := defaultSplitMaxLines
	fix := false
	jsonOut := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--fix":
			fix = true
		case a == "--json":
			jsonOut = true
		case a == "--max":
			if i+1 < len(args) {
				var n int
				_, _ = fmt.Sscanf(args[i+1], "%d", &n)
				if n > 0 {
					maxSize = n
				}
				i++
			}
		case !strings.HasPrefix(a, "--"):
			root = a
		}
	}

	files, err := collectGoFiles(root)
	if err != nil {
		return err
	}

	rules := defaultLintRules()
	ctx := LintContext{Root: root, Files: files, MaxSize: maxSize}
	var issues []lintIssue
	for _, rule := range rules {
		issues = append(issues, rule.Run(ctx)...)
	}

	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]interface{}{
			"issues": issues,
			"summary": map[string]int{
				"total": len(issues),
			},
		})
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

	if fix {
		applied, failed := applyAutoFixes(issues, ctx, rules)
		fmt.Printf("\nAuto-fixes: %d applied, %d failed\n", applied, failed)
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
	Root    string
	Files   []string
	MaxSize int
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
