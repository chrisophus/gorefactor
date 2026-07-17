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
			iss := lintIssue{
				File:     f,
				Rule:     "complexity",
				Severity: sev,
				Message:  fmt.Sprintf("%s has cyclomatic complexity %d (threshold %d, line %d) — consider extracting", c.Name, c.Complexity, defaultComplexityThreshold, c.Line),
			}
			// Only offer the autofix when there is at least one block to shed;
			// a function whose complexity is pure straight-line branching has no
			// extractable top-level block (RecommendComplexityReduction returns
			// none), and the extract engine additionally refuses return-bearing
			// blocks, so the fix is advertised optimistically and best-effort.
			// Extraction autofix disabled: the automated extraction engine
			// produces unreliable output (name collisions, invalid signatures).
			// TODO: re-enable once the extractor is hardened.
			out = append(out, iss)
		}
	}
	return out
}
