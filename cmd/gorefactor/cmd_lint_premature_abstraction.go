package main

import (
	"path/filepath"

	"github.com/chrisophus/gorefactor/analyzer"
)

type prematureAbstractionRule struct{}

func (prematureAbstractionRule) Name() string { return "premature-abstraction" }

func (r prematureAbstractionRule) Run(ctx LintContext) []lintIssue {
	dirSet := make(map[string]bool, len(ctx.Files))
	for _, f := range ctx.Files {
		dirSet[filepath.Dir(f)] = true
	}
	dirs := make([]string, 0, len(dirSet))
	for d := range dirSet {
		dirs = append(dirs, d)
	}
	issues, err := analyzer.FindPrematureAbstractionsInDirs(dirs)
	if err != nil {
		return nil
	}
	var out []lintIssue
	for _, e := range issues {
		out = append(out, lintIssue{
			File:     e.File,
			Rule:     "premature-abstraction",
			Severity: "info",
			Message:  e.Message,
		})
	}
	return out
}
