package main

import (
	"fmt"
	"strings"

	"github.com/chrisophus/gorefactor/analyzer"
)

func checkDuplicates(root string, walk analyzer.WalkOptions) []lintIssue {
	blocks, err := analyzer.FindDuplicateBlocksInDir(root, walk)
	if err != nil {
		return nil
	}
	var out []lintIssue
	for _, d := range blocks {
		if d.ImpactScore < analyzer.MinDuplicateImpactScore {
			continue
		}
		// Duplication confined entirely to test files is idiomatic (table
		// fixtures, parallel setup) — only flag when at least one occurrence
		// is in production code.
		if allTestLocations(d.Locations) {
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

// allTestLocations reports whether every duplicate occurrence lives in a
// _test.go file.
func allTestLocations(locs []analyzer.Location) bool {
	for _, l := range locs {
		if !isTestFile(l.File) {
			return false
		}
	}
	return len(locs) > 0
}
