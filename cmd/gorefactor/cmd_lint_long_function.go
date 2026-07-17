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
		threshold := longFunctionThreshold
		if isTestFile(f) {
			threshold *= longFunctionTestFactor
		}
		for _, m := range metrics {
			if m.Lines < threshold {
				continue
			}
			iss := lintIssue{
				File:     f,
				Rule:     "long-function",
				Severity: "warning",
				Message:  fmt.Sprintf("%s is %d lines (threshold %d, line %d) — consider extracting", m.Key(), m.Lines, threshold, m.Line),
			}
			// Extraction autofix disabled: the automated extraction engine
			// produces unreliable output (name collisions, invalid signatures).
			// TODO: re-enable once the extractor is hardened.
			out = append(out, iss)
		}
	}
	return out

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
