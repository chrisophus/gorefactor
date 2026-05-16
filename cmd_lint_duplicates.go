package main

import (
	"fmt"
	"gorefactor/analyzer"
	"strings"
)

func checkDuplicates(root string) []lintIssue {
	result, err := analyzer.AnalyzeCrossFile(root)
	if err != nil || result == nil {
		return nil
	}
	var out []lintIssue
	for _, d := range result.DuplicateBlocks {
		if d.ImpactScore < 5 {
			continue
		}
		locs := make([]string, 0, len(d.Locations))
		for _, l := range d.Locations {
			locs = append(locs, fmt.Sprintf("%s:%d-%d", l.File, l.StartLine, l.EndLine))
		}
		out = append(out, lintIssue{
			File:     d.Locations[0].File,
			Rule:     "duplicate-block",
			Severity: "warning",
			Message:  fmt.Sprintf("%d-stmt block duplicated in %d places (impact %d): %s", d.StatementCount, len(d.Locations), d.ImpactScore, strings.Join(locs, ", ")),
		})
	}
	return out
}
