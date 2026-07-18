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
		threshold := defaultComplexityThreshold
		if isTestFile(f) {
			threshold *= longFunctionTestFactor
		}
		for _, c := range complexities {
			if c.Complexity <= threshold {
				continue
			}

			if d := c.Dispatch; d != nil && d.NormalizedComplexity <= threshold {
				out = append(out, lintIssue{
					File:     f,
					Rule:     "complexity",
					Severity: "info",
					Message: fmt.Sprintf("%s has cyclomatic complexity %d (threshold %d, line %d) — dispatch table: %d cases, worst case %d; per-branch score %d is under threshold",
						c.Name, c.Complexity, threshold, c.Line, d.Cases, d.WorstCaseComplexity, d.NormalizedComplexity),
				})
				continue
			}

			sev := "warning"
			if c.Complexity > threshold*2 {
				sev = "error"
			}
			iss := lintIssue{
				File:     f,
				Rule:     "complexity",
				Severity: sev,
				Message:  fmt.Sprintf("%s has cyclomatic complexity %d (threshold %d, line %d) — consider extracting", c.Name, c.Complexity, threshold, c.Line),
			}
			// Live no-target check, mirroring long-function's: don't imply
			// extraction will help when the reducer has nothing to offer.
			if !hasViableComplexityExtraction(f, c.Name, threshold) {
				iss.Note = "no mechanical extraction applies — needs restructuring, not extraction"
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
