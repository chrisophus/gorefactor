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

type duplicateRule struct{}

func (duplicateRule) Name() string { return "duplicate-block" }

func (r duplicateRule) Run(ctx LintContext) []lintIssue {
	return checkDuplicates(ctx.Root, ctx.WalkOpts)
}
