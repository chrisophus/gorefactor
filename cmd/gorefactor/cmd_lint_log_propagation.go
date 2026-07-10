package main

import (
	"fmt"
	"strings"

	"github.com/chrisophus/gorefactor/analyzer"
)

type ifErrLogReturnRule struct{}

func (ifErrLogReturnRule) Name() string { return "if-err-log-return" }

func (r ifErrLogReturnRule) Run(ctx LintContext) []lintIssue {
	return fileLogPropagationIssues(ctx, analyzer.FileIfErrLogReturnIssues)
}

type wrapLogReturnRule struct{}

func (wrapLogReturnRule) Name() string { return "wrap-log-return" }

func (r wrapLogReturnRule) Run(ctx LintContext) []lintIssue {
	return fileLogPropagationIssues(ctx, analyzer.FileWrapLogReturnIssues)
}

type wrapBridgeLogReturnRule struct{}

func (wrapBridgeLogReturnRule) Name() string { return "wrap-bridge-log-return" }

func (r wrapBridgeLogReturnRule) Run(ctx LintContext) []lintIssue {
	return fileLogPropagationIssues(ctx, analyzer.FileWrapBridgeLogReturnIssues)
}

type duplicateBareSentinelRule struct{}

func (duplicateBareSentinelRule) Name() string { return "duplicate-bare-sentinel" }

func (r duplicateBareSentinelRule) Run(ctx LintContext) []lintIssue {
	byDir := analyzer.GroupFilesByDir(ctx.Files)
	var out []lintIssue
	for _, files := range byDir {
		issues, err := analyzer.PackageDuplicateBareSentinelIssues(files)
		if err != nil {
			continue
		}
		for _, iss := range issues {
			li := logPropagationToLintIssue(iss)
			if iss.Symbol != "" {
				li.AutoFix = "wrap-sentinels"
				li.AutoFixCmd = fmt.Sprintf("wrap-sentinels %s %s", iss.File, iss.Symbol)
			}
			out = append(out, li)
		}
	}
	return out

}

type fileLogFn func(file string) ([]analyzer.LogPropagationIssue, error)

func fileLogPropagationIssues(ctx LintContext, fn fileLogFn) []lintIssue {
	var out []lintIssue
	for _, f := range ctx.Files {
		if analyzer.ShouldSkipFile(f, ctx.WalkOpts) {
			continue
		}
		issues, err := fn(f)
		if err != nil || len(issues) == 0 {
			continue
		}
		fixable := fixableLogReturnSites(f)
		for _, iss := range issues {
			li := logPropagationToLintIssue(iss)
			if fixable[fmt.Sprintf("%s:%d:%d", iss.Rule, iss.Line, iss.Column)] {
				li.AutoFix = "remove-log-return"
				li.AutoFixCmd = fmt.Sprintf("remove-log-return %s --rule %s", f, iss.Rule)
			}
			out = append(out, li)
		}
	}
	return out

}

func fixableLogReturnSites(f string) map[string]bool {
	sites, err := analyzer.ListLogReturnFixSites(f)
	if err != nil {
		return nil
	}
	m := make(map[string]bool, len(sites))
	for _, s := range sites {
		m[fmt.Sprintf("%s:%d:%d", s.Rule, s.Line, s.Column)] = true
	}
	return m
}

func runLogPropagationAutoFix(issue lintIssue, cmdName string, run func([]string) error) error {
	parts := strings.Fields(issue.AutoFixCmd)
	if len(parts) < 2 || parts[0] != cmdName {
		return fmt.Errorf("malformed autofix command: %q", issue.AutoFixCmd)
	}
	return run(parts[1:])
}

func logPropagationToLintIssue(iss analyzer.LogPropagationIssue) lintIssue {
	loc := iss.File
	if iss.Line > 0 {
		loc = fmt.Sprintf("%s:%d:%d", iss.File, iss.Line, iss.Column)
	}
	return lintIssue{
		File:     loc,
		Rule:     iss.Rule,
		Severity: "error",
		Message:  iss.Message,
	}
}

// logPropagationRules returns Marketplace-style log propagation lint rules.
func logPropagationRules() []LintRule {
	return []LintRule{
		ifErrLogReturnRule{},
		wrapLogReturnRule{},
		wrapBridgeLogReturnRule{},
		duplicateBareSentinelRule{},
	}
}

func (r ifErrLogReturnRule) AutoFix(issue lintIssue, _ LintContext) error {
	return runLogPropagationAutoFix(issue, "remove-log-return", removeLogReturnCommand)
}

func (r wrapLogReturnRule) AutoFix(issue lintIssue, _ LintContext) error {
	return runLogPropagationAutoFix(issue, "remove-log-return", removeLogReturnCommand)
}

func (r wrapBridgeLogReturnRule) AutoFix(issue lintIssue, _ LintContext) error {
	return runLogPropagationAutoFix(issue, "remove-log-return", removeLogReturnCommand)
}

func (r duplicateBareSentinelRule) AutoFix(issue lintIssue, _ LintContext) error {
	return runLogPropagationAutoFix(issue, "wrap-sentinels", wrapSentinelsCommand)
}
