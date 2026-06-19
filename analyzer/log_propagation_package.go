package analyzer

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// PackageDuplicateBareSentinelIssues flags bare returns of the same errors.New sentinel.
func PackageDuplicateBareSentinelIssues(files []string) ([]LogPropagationIssue, error) {
	if len(files) == 0 {
		return nil, nil
	}
	fset := token.NewFileSet()
	var astFiles []*ast.File
	var paths []string
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			continue
		}
		astFiles = append(astFiles, f)
		paths = append(paths, path)
	}
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
				Rule: "duplicate-bare-sentinel", Message: msg,
			})
		}
	}
	return out, nil
}
