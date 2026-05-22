package main

import (
	"fmt"

	"github.com/chrisophus/gorefactor/analyzer"
)

const defaultComplexityThreshold = 15

type complexityRule struct{}

func (complexityRule) Name() string { return "complexity" }

func (r complexityRule) Run(ctx LintContext) []lintIssue {
	var out []lintIssue
	for _, f := range ctx.Files {
		complexities, err := analyzer.FileFunctionComplexities(f)
		if err != nil {
			continue
		}
		for _, c := range complexities {
			if c.Complexity <= defaultComplexityThreshold {
				continue
			}
			sev := "warning"
			if c.Complexity > defaultComplexityThreshold*2 {
				sev = "error"
			}
			out = append(out, lintIssue{
				File:     f,
				Rule:     "complexity",
				Severity: sev,
				Message:  fmt.Sprintf("%s has cyclomatic complexity %d (threshold %d, line %d) — consider extracting", c.Name, c.Complexity, defaultComplexityThreshold, c.Line),
			})
		}
	}
	return out
}
