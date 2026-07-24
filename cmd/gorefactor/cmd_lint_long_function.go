package main

import (
	"fmt"
	"strings"

	"github.com/chrisophus/gorefactor/analyzer"
)

// longFunctionThreshold is the line count above which a function is flagged.
const longFunctionThreshold = analyzer.DefaultLongFunctionLines

type longFunctionRule struct{}

func (longFunctionRule) Name() string { return "long-function" }

func (r longFunctionRule) Run(ctx LintContext) []lintIssue {
	var out []lintIssue
	for _, f := range ctx.Files {
		metrics, err := analyzer.FunctionMetricsForFile(f)
		if err != nil {
			continue
		}
		threshold := longFunctionThreshold
		if isTestFile(f) {
			threshold *= longFunctionTestFactor
		}
		for _, m := range metrics {
			if m.LogicLines() < threshold {
				continue
			}

			if d := m.Dispatch; d != nil && m.LogicLines()-d.LineDiscount < threshold {
				out = append(out, lintIssue{
					File:     f,
					Rule:     "long-function",
					Severity: "info",
					Message: fmt.Sprintf("%s is %d lines (threshold %d, line %d) — dispatch table: %d cases, worst case %d lines; per-branch length %d is under threshold",

						// Measure logic lines, not data: a declarative catalog of composite
						// literals is long in data, not complexity, and extracting it helps
						// no one. LogicLines subtracts the literal span.
						m.Key(), m.Lines, threshold, m.Line, d.Cases, d.WorstCaseLines, m.LogicLines()-d.LineDiscount),
				})
				continue
			}

			iss := lintIssue{
				File:      f,
				Rule:      "long-function",
				Severity:  "info", // single-axis proxy; hard-to-maintain is the gate
				Message:   fmt.Sprintf("%s is %d lines (threshold %d, line %d) — consider extracting", m.Key(), m.Lines, threshold, m.Line),
				Value:     m.Lines,
				Threshold: threshold,
			}
			// Live no-target check: if the reducer has no nameable,
			// non-vacuous block to offer, say so up front instead of
			// implying extraction will help.
			if !hasViableLengthExtraction(f, m.Key(), threshold) {
				iss.Note = "no mechanical extraction applies — needs restructuring, not extraction"
			}
			// Extraction autofix is aggressive-only (needs generated helper
			// names) and therefore always verify-gated. The extractor now names
			// blocks from their leading comment/structure and correctly handles
			// return-lifting tails and type-switch bindings; the gate catches
			// any residual bad lift and reverts it.
			if ctx.AggressiveFix() {
				iss.AutoFix = "extract blocks to reduce length (aggressive)"
				iss.AutoFixCmd = fmt.Sprintf("gorefactor recommend --reduce-length %s %s --max-lines %d --apply --allow-returns",
					f, m.Key(), threshold)
			}
			out = append(out, iss)
		}
	}
	return out

}

// AutoFix extracts top-level blocks to bring the function under threshold. It
// mirrors extract-candidate's fixer and shares its reduction path; it is only
// wired at the aggressive fix level, so it is always verify-gated.
func (r longFunctionRule) AutoFix(issue lintIssue, _ LintContext) error {
	return reduceLengthAutoFix("long-function", issue)

}

func parseReduceLengthAutoFixCmd(cmd string) (file, function string, ok bool) {
	fields := strings.Fields(cmd)
	for i, f := range fields {
		if f == "--reduce-length" && i+2 < len(fields) {
			return fields[i+1], fields[i+2], true
		}
	}
	return "", "", false
}

func suggestionsOf[E any](xs []E, name func(E) string) []string {
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		out = append(out, name(x))
	}
	return out
}

func anyNameable(names []string) bool {
	for _, n := range names {
		if !analyzer.IsGeneratedFallbackName(n) {
			return true
		}
	}
	return false
}

// hasViableLengthExtraction reports whether the length reducer can propose at least one nameable,
// non-vacuous block for the function — the same filter the autofix applies. Cheap (single-file
// analysis, no mutation); the size rules use it to stop advertising "consider extracting" on
// findings the fixer has nothing to offer. An analysis error keeps the default message: unknown is
// not hopeless.
func hasViableLengthExtraction(file, function string, maxLines int) bool {
	res, err := analyzer.RecommendLengthReduction(file, function, maxLines)
	return err != nil || anyNameable(suggestionsOf(res.Extractions, func(e analyzer.LengthExtraction) string { return e.Suggestion }))

}

// hasViableComplexityExtraction is the complexity analog of hasViableLengthExtraction.
func hasViableComplexityExtraction(file, function string, threshold int) bool {
	res, err := analyzer.RecommendComplexityReduction(file, function, threshold)
	return err != nil || anyNameable(suggestionsOf(res.Extractions, func(e analyzer.ComplexityExtraction) string { return e.Suggestion }))

}

func reduceLengthAutoFix(ruleName string, issue lintIssue) error {
	file, function, ok := parseReduceLengthAutoFixCmd(issue.AutoFixCmd)
	if !ok {
		return fmt.Errorf("malformed %s autofix command: %q", ruleName, issue.AutoFixCmd)
	}
	metrics, err := analyzer.FunctionMetricsForFile(file)
	if err != nil {
		return fmt.Errorf("re-derive function length: %w", err)
	}
	lines := 0
	for _, m := range metrics {
		if m.Key() == function {
			lines = m.Lines
			break
		}
	}
	if lines == 0 {
		return fmt.Errorf("%s: function no longer present in %s", function, file)
	}
	applied, err := reduceLengthByExtraction(file, function, lines-1, true)
	if err != nil {
		return fmt.Errorf("reduce length by extraction: %w", err)
	}
	if applied == 0 {
		return fmt.Errorf("%s: no extractable top-level blocks", function)
	}
	return nil
}
