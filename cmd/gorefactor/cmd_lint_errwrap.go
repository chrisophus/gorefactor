package main

import (
	"github.com/chrisophus/gorefactor/analyzer"
)

type errWrapRule struct{}

func (errWrapRule) Name() string { return "error-not-wrapped" }

func (r errWrapRule) Run(ctx LintContext) []lintIssue {
	var out []lintIssue
	for _, f := range ctx.Files {
		issues, err := analyzer.FileErrorWrapIssues(f)
		if err != nil {
			continue
		}
		for _, e := range issues {
			out = append(out, lintIssue{
				File:     f,
				Rule:     "error-not-wrapped",
				Severity: "warning",
				Message:  e.Message,
			})
		}
	}
	return out
}
