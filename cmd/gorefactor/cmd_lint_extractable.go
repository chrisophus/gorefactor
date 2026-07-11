package main

import (
	"fmt"

	"github.com/chrisophus/gorefactor/analyzer"
)

// checkExtractable surfaces gorefactor's own extraction recommendations as
// informational lint issues. At the safe fix level there is no autofix
// because extraction requires naming the new method, which needs human
// judgment; the aggressive level accepts generated names in exchange for the
// verify gate. (Signature edited directly: change-signature requires a
// type-checking module, which the new body prevents mid-edit.)
func checkExtractable(file string, minPriority int, aggressive bool) []lintIssue {
	issue, err := analyzer.AnalyzeFileSize(file, 0)
	if err != nil {
		return nil
	}
	var out []lintIssue
	for _, h := range issue.ExtractionHints {
		if h.Priority < minPriority {
			continue
		}
		iss := lintIssue{
			File:     file,
			Rule:     "extract-candidate",
			Severity: "info",
			Message: fmt.Sprintf("%s (lines %d-%d, %d lines, complexity %d, priority %d/10) — consider extracting a method",
				h.FunctionName, h.StartLine, h.EndLine, h.LineCount, h.Complexity, h.Priority),
		}
		// Naming the new method needs human judgment, so the autofix (extract
		// the function's largest top-level block under a generated name) is
		// aggressive-only and therefore always verify-gated.
		if aggressive {
			iss.AutoFix = "extract largest block (aggressive)"
			iss.AutoFixCmd = fmt.Sprintf("gorefactor recommend --reduce-length %s %s --max-lines %d --apply --allow-returns",
				file, h.FunctionName, h.LineCount-1)
		}
		out = append(out, iss)
	}
	return out

}

const defaultExtractMinPriority = 8

type extractableRule struct{}

func (extractableRule) Name() string { return "extract-candidate" }

func (r extractableRule) Run(ctx LintContext) []lintIssue {
	var out []lintIssue
	for _, f := range ctx.Files {
		out = append(out, checkExtractable(f, defaultExtractMinPriority, ctx.AggressiveFix())...)
	}
	return out
}

func (r extractableRule) AutoFix(issue lintIssue, _ LintContext) error {
	file, function, ok := parseReduceLengthAutoFixCmd(issue.AutoFixCmd)
	if !ok {
		return fmt.Errorf("malformed extract-candidate autofix command: %q", issue.AutoFixCmd)
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
