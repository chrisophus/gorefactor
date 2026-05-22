package main

import (
	"path/filepath"

	"github.com/chrisophus/gorefactor/analyzer"
)

type prematureAbstractionRule struct{}

func (prematureAbstractionRule) Name() string { return "premature-abstraction" }

func (r prematureAbstractionRule) Run(ctx LintContext) []lintIssue {
	dirs := make(map[string]bool, len(ctx.Files))
	for _, f := range ctx.Files {
		dirs[filepath.Dir(f)] = true
	}
	var out []lintIssue
	for dir := range dirs {
		issues, err := analyzer.FindPrematureAbstractionsInDir(dir)
		if err != nil {
			continue
		}
		for _, e := range issues {
			out = append(out, lintIssue{
				File:     e.File,
				Rule:     "premature-abstraction",
				Severity: "info",
				Message:  e.Message,
			})
		}
	}
	return out
}
