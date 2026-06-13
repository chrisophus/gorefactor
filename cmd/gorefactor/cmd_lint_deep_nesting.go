package main

import (
	"fmt"

	"github.com/chrisophus/gorefactor/analyzer"
)

// maxNestingThreshold is the maximum control-structure nesting depth before
// a function is flagged.
const maxNestingThreshold = 5

type deepNestingRule struct{}

func (deepNestingRule) Name() string { return "deep-nesting" }

func (r deepNestingRule) Run(ctx LintContext) []lintIssue {
	var out []lintIssue
	for _, f := range ctx.Files {
		metrics, err := analyzer.FunctionMetricsForFile(f)
		if err != nil {
			continue
		}
		for _, m := range metrics {
			if m.MaxNesting <= maxNestingThreshold {
				continue
			}
			out = append(out, lintIssue{
				File:     f,
				Rule:     "deep-nesting",
				Severity: "warning",
				Message:  fmt.Sprintf("%s has nesting depth %d (threshold %d, line %d) — consider extracting inner blocks", m.Key(), m.MaxNesting, maxNestingThreshold, m.Line),
			})
		}
	}
	return out
}
