package main

import (
	"fmt"

	"github.com/chrisophus/gorefactor/analyzer"
)

func checkFileSize(file string, maxSize int) []lintIssue {
	issue, err := analyzer.AnalyzeFileSize(file, maxSize)
	if err != nil || !issue.IsOversized {
		return nil
	}
	return []lintIssue{{
		File:       file,
		Rule:       "file-size",
		Severity:   "error",
		Message:    fmt.Sprintf("%d lines (limit %d, over by %d)", issue.LineCount, issue.MaxRecommended, issue.OverageSize),
		AutoFix:    "split file",
		AutoFixCmd: fmt.Sprintf("gorefactor split %s --max %d", file, maxSize),
	}}
}

type fileSizeRule struct{}

func (fileSizeRule) Name() string { return "file-size" }

func (r fileSizeRule) Run(ctx LintContext) []lintIssue {
	var out []lintIssue
	for _, f := range ctx.Files {
		if analyzer.ShouldSkipFile(f, ctx.WalkOpts) {
			continue
		}
		max := effectiveMaxSizeForFile(f, ctx)
		out = append(out, checkFileSize(f, max)...)
	}
	return out
}

func (r fileSizeRule) AutoFix(issue lintIssue, ctx LintContext) error {
	// Use the same effective threshold as Run: a caller that never set
	// MaxSize (e.g. doctor --fix constructing options directly) would
	// otherwise pass --max 0 to split.
	max := effectiveMaxSizeForFile(issue.File, ctx)
	return splitCommand([]string{issue.File, "--max", fmt.Sprintf("%d", max)})

}
