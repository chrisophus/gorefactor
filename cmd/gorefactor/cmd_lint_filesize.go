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
	sev := "warning"
	if issue.OverageSize > maxSize/2 {
		sev = "error"
	}
	return []lintIssue{{
		File:       file,
		Rule:       "file-size",
		Severity:   sev,
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
		out = append(out, checkFileSize(f, ctx.MaxSize)...)
	}
	return out
}

func (r fileSizeRule) AutoFix(issue lintIssue, ctx LintContext) error {
	return splitCommand([]string{issue.File, "--max", fmt.Sprintf("%d", ctx.MaxSize)})
}
