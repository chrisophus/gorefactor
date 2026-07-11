package main

import (
	"fmt"
	"strings"

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
			iss := lintIssue{
				File:     f,
				Rule:     "long-function",
				Severity: "warning",
				Message:  fmt.Sprintf("%s is %d lines (threshold %d, line %d) — consider extracting", m.Key(), m.Lines, longFunctionThreshold, m.Line),
			}
			// Shortening a function means extracting blocks under generated
			// names — mechanical but not judgment-free, so the autofix is
			// aggressive-only (and therefore always verify-gated). Offered only
			// when the reducer actually finds a block to shed.
			if ctx.AggressiveFix() {
				if red, rerr := analyzer.RecommendLengthReduction(f, m.Key(), longFunctionThreshold); rerr == nil && len(red.Extractions) > 0 {
					iss.AutoFix = "extract sub-blocks (aggressive)"
					iss.AutoFixCmd = fmt.Sprintf("gorefactor recommend --reduce-length %s %s --max-lines %d --apply --allow-returns", f, m.Key(), longFunctionThreshold)
				}
			}
			out = append(out, iss)
		}
	}
	return out

}

func (r longFunctionRule) AutoFix(issue lintIssue, _ LintContext) error {
	file, function, ok := parseReduceLengthAutoFixCmd(issue.AutoFixCmd)
	if !ok {
		return fmt.Errorf("malformed long-function autofix command: %q", issue.AutoFixCmd)
	}
	applied, err := reduceLengthByExtraction(file, function, longFunctionThreshold, true)
	if err != nil {
		return fmt.Errorf("reduce length by extraction: %w", err)
	}
	if applied == 0 {
		return fmt.Errorf("%s: no extractable top-level blocks", function)
	}
	return nil
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
