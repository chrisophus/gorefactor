package main

import (
	"fmt"

	"github.com/chrisophus/gorefactor/analyzer"
)

type funcorderConstructorRule struct{}

func (funcorderConstructorRule) Name() string { return "funcorder-constructor" }

func (r funcorderConstructorRule) Run(ctx LintContext) []lintIssue {
	return fileFuncorderIssues(ctx, "funcorder-constructor")
}

func (r funcorderConstructorRule) AutoFix(issue lintIssue, _ LintContext) error {
	return runFuncorderAutoFix(issue)
}

type funcorderStructMethodRule struct{}

func (funcorderStructMethodRule) Name() string { return "funcorder-struct-method" }

func (r funcorderStructMethodRule) Run(ctx LintContext) []lintIssue {
	return fileFuncorderIssues(ctx, "funcorder-struct-method")
}

func (r funcorderStructMethodRule) AutoFix(issue lintIssue, _ LintContext) error {
	return runFuncorderAutoFix(issue)
}

func fileFuncorderIssues(ctx LintContext, rule string) []lintIssue {
	var out []lintIssue
	for _, f := range ctx.Files {
		if analyzer.ShouldSkipFile(f, ctx.WalkOpts) {
			continue
		}
		issues, err := analyzer.FileFuncorderIssues(f)
		if err != nil {
			continue
		}
		for _, iss := range issues {
			if iss.Rule != rule {
				continue
			}
			out = append(out, lintIssue{
				File:       fmt.Sprintf("%s:%d:%d", iss.File, iss.Line, iss.Column),
				Rule:       iss.Rule,
				Severity:   "warning",
				Message:    iss.Message,
				AutoFix:    "reorder-funcorder",
				AutoFixCmd: fmt.Sprintf("reorder-funcorder %s", f),
			})
		}
	}
	return out
}

type funcorderFunctionRule struct{}

func (funcorderFunctionRule) Name() string { return "funcorder-function" }

func (r funcorderFunctionRule) Run(ctx LintContext) []lintIssue {
	return fileFuncorderIssues(ctx, "funcorder-function")
}

func (r funcorderFunctionRule) AutoFix(issue lintIssue, _ LintContext) error {
	return runFuncorderAutoFix(issue)
}

func runFuncorderAutoFix(issue lintIssue) error {
	return runLogPropagationAutoFix(issue, "reorder-funcorder", reorderFuncorderCommand)
}

// funcorderRules returns the funcorder-derived lint rules (constructor
// placement, exported-before-unexported method ordering, and
// exported-before-unexported top-level function ordering).
func funcorderRules() []LintRule {
	return []LintRule{
		funcorderConstructorRule{},
		funcorderStructMethodRule{},
		funcorderFunctionRule{},
	}
}
