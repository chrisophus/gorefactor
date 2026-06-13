package main

import (
	"fmt"

	"github.com/chrisophus/gorefactor/analyzer"
)

// longFunctionThreshold is the line count above which a function is flagged.
const longFunctionThreshold = 75

type longFunctionRule struct{}

func (longFunctionRule) Name() string { return "long-function" }

func (r longFunctionRule) Run(ctx LintContext) []lintIssue {
	var out []lintIssue
	for _, f := range ctx.Files {
		metrics, err := analyzer.FunctionMetricsForFile(f)
		if err != nil {
			continue
		}
		for _, m := range metrics {
			if m.Lines < longFunctionThreshold {
				continue
			}
			out = append(out, lintIssue{
				File:       f,
				Rule:       "long-function",
				Severity:   "warning",
				Message:    fmt.Sprintf("%s is %d lines (threshold %d, line %d) — consider extracting", m.Key(), m.Lines, longFunctionThreshold, m.Line),
				AutoFixCmd: fmt.Sprintf("gorefactor recommend %s --function %s", f, m.Key()),
			})
		}
	}
	return out
}
