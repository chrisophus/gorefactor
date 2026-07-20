package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/chrisophus/gorefactor/analyzer"
)

// highBlastRadiusTransitive is the transitive-caller count at or above which a
// function is "load-bearing": a change ripples to at least this many other
// functions, so it warrants test coverage before being refactored.
const highBlastRadiusTransitive = 20

// highBlastRadiusMaxFindings caps how many load-bearing functions the rule
// reports. This is a ranking/awareness signal, not a defect list: the point is
// "here are the riskiest functions to change", so only the highest-scoring few
// are actionable. Reporting every function over the threshold (name-based
// transitive counting over-approximates, so that can be hundreds) is pure
// noise. Surface the top slice by blast score.
const highBlastRadiusMaxFindings = 10

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

	type finding struct {
		iss   lintIssue
		score int
	}
	var found []finding
	for key, def := range idx.Defs {
		if strings.HasSuffix(def.File, "_test.go") {
			continue
		}
		if len(idx.TransitiveCallers(def)) < highBlastRadiusTransitive {
			continue
		}
		br := computeBlastRadius(idx, def)
		found = append(found, finding{
			score: br.Score,
			iss: lintIssue{
				File:     def.File,
				Rule:     "high-blast-radius",
				Severity: "info",
				Message: fmt.Sprintf(
					"%s is load-bearing: %d transitive callers across %d files (blast score %d) — changes here ripple widely; ensure tests cover it before refactoring",
					key, br.TransitiveCallers, br.FilesAffected, br.Score,
				),
			},
		})
	}
	// Rank by blast score (ties broken by file for determinism) and keep only
	// the most load-bearing few — this is an awareness signal, not a backlog.
	sort.Slice(found, func(i, j int) bool {
		if found[i].score != found[j].score {
			return found[i].score > found[j].score
		}
		return found[i].iss.File < found[j].iss.File
	})
	if len(found) > highBlastRadiusMaxFindings {
		found = found[:highBlastRadiusMaxFindings]
	}
	out := make([]lintIssue, 0, len(found))
	for _, f := range found {
		out = append(out, f.iss)
	}
	return out
}
