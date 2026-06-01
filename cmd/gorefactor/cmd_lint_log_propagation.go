package main

import (
	"fmt"

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
			out = append(out, logPropagationToLintIssue(iss))
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
		if err != nil {
			continue
		}
		for _, iss := range issues {
			out = append(out, logPropagationToLintIssue(iss))
		}
	}
	return out
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
