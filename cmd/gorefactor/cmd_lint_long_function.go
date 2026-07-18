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
			// Measure logic lines, not data: a declarative catalog of composite
			// literals is long in data, not complexity, and extracting it helps
			// no one. LogicLines subtracts the literal span.
			if m.LogicLines() < threshold {
				continue
			}
			iss := lintIssue{
				File:     f,
				Rule:     "long-function",
				Severity: "warning",
				Message:  fmt.Sprintf("%s is %d lines (threshold %d, line %d) — consider extracting", m.Key(), m.Lines, threshold, m.Line),
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
