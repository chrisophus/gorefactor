package analyzer

import (
	"fmt"
)

// PackageDuplicateBareSentinelIssues flags bare returns of the same errors.New sentinel.
func PackageDuplicateBareSentinelIssues(files []string) ([]LogPropagationIssue, error) {
	if len(files) == 0 {
		return nil, nil
	}
	fset, astFiles, paths := parseNonTestFiles(files)
	if len(astFiles) == 0 {
		return nil, nil
	}
	sentinels := collectErrorsNewSentinels(astFiles, paths)
	bare := bareSentinelReturnPositions(astFiles, paths, fset, sentinels)
	var out []LogPropagationIssue
	for name, positions := range bare {
		if len(positions) < 2 {
			continue
		}
		msg := fmt.Sprintf(
			"duplicate bare return of %s (%d sites in package); wrap each with fmt.Errorf(\"…: %%w\", %s)",
			name, len(positions), name)
		for _, pos := range positions {
			out = append(out, LogPropagationIssue{
				File: pos.Filename, Line: pos.Line, Column: pos.Column,
				Rule: "duplicate-bare-sentinel", Message: msg, Symbol: name,
			})
		}
	}
	return out, nil

}
