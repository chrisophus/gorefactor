package main

import (
	"fmt"
	"strings"

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
			autoFixCmd := fmt.Sprintf("wrap-errors %s %s", f, e.Function)
			out = append(out, lintIssue{
				File:       f,
				Rule:       "error-not-wrapped",
				Severity:   "warning",
				Message:    e.Message,
				AutoFix:    "wrap-errors",
				AutoFixCmd: autoFixCmd,
			})
		}
	}
	return out
}

// AutoFix implements FixableRule: runs the wrap-errors command for the issue.
func (r errWrapRule) AutoFix(issue lintIssue, _ LintContext) error {
	if issue.AutoFixCmd == "" {
		return nil
	}
	parts := strings.Fields(issue.AutoFixCmd)
	if len(parts) < 2 {
		return fmt.Errorf("invalid autofixCmd: %q", issue.AutoFixCmd)
	}
	// parts[0] = "wrap-errors", rest = args for the wrap-errors command.
	return wrapErrorsCommand(parts[1:])

}
