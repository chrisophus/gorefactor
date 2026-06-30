package main

import (
	"fmt"
	"strings"

	"github.com/chrisophus/gorefactor/analyzer"
)

// highBlastRadiusTransitive is the transitive-caller count at or above which a
// function is "load-bearing": a change ripples to at least this many other
// functions, so it warrants test coverage before being refactored.
const highBlastRadiusTransitive = 20

type blastRadiusRule struct{}

func (blastRadiusRule) Name() string { return "high-blast-radius" }

// Run flags load-bearing functions — those with a large transitive-caller set.
// It is an awareness sensor (info severity, no autofix): there is no single
// safe transformation, the point is to warn before a high-impact change. Test
// functions are skipped; they are not load-bearing production code.
func (r blastRadiusRule) Run(ctx LintContext) []lintIssue {
	files := ctx.Files
	if len(files) == 0 {
		var err error
		files, err = collectGoFiles(ctx.Root, analyzer.DefaultWalkOptions())
		if err != nil {
			return nil
		}
	}
	idx, err := buildCallIndex(files)
	if err != nil {
		return nil
	}

	var out []lintIssue
	for key, def := range idx.defs {
		if strings.HasSuffix(def.file, "_test.go") {
			continue
		}
		if len(idx.transitiveCallers(def)) < highBlastRadiusTransitive {
			continue
		}
		br := computeBlastRadius(idx, def)
		out = append(out, lintIssue{
			File:     def.file,
			Rule:     "high-blast-radius",
			Severity: "info",
			Message: fmt.Sprintf(
				"%s is load-bearing: %d transitive callers across %d files (blast score %d) — changes here ripple widely; ensure tests cover it before refactoring",
				key, br.TransitiveCallers, br.FilesAffected, br.Score,
			),
		})
	}
	return out
}
