package main

import (
	"fmt"

	"gorefactor/analyzer"
)

// checkExtractable surfaces gorefactor's own extraction recommendations
// as informational lint issues. No autofix because extraction requires
// naming the new method, which needs human judgment.
func checkExtractable(file string, minPriority int) []lintIssue {
	issue, err := analyzer.AnalyzeFileSize(file, 0)
	if err != nil {
		return nil
	}
	var out []lintIssue
	for _, h := range issue.ExtractionHints {
		if h.Priority < minPriority {
			continue
		}
		out = append(out, lintIssue{
			File:     file,
			Rule:     "extract-candidate",
			Severity: "info",
			Message: fmt.Sprintf("%s (lines %d-%d, %d lines, complexity %d, priority %d/10) — consider extracting a method",
				h.FunctionName, h.StartLine, h.EndLine, h.LineCount, h.Complexity, h.Priority),
		})
	}
	return out
}
