package main

import (
	"go/parser"
	"go/token"

	"github.com/chrisophus/gorefactor/analyzer"
)

// checkSmells parses a Go file and detects architectural patterns and code smells
func checkSmells(file string) []lintIssue {
	fset := token.NewFileSet()
	astFile, err := parser.ParseFile(fset, file, nil, 0)
	if err != nil {
		return nil
	}

	patterns := analyzer.NewPatternDetector(astFile).DetectPatterns()
	var issues []lintIssue
	for _, p := range patterns {
		issue := lintIssue{
			File:     file,
			Rule:     "smell",
			Severity: p.Severity,
			Message:  p.Name + ": " + p.Description,
		}
		issues = append(issues, issue)
	}
	return issues
}

// checkDeadCode detects unused unexported functions/methods per Go package
// directory. Whole-tree analysis misattributes package-local symbols; one
// detector per directory matches Go's package boundary and is much faster.
func checkDeadCode(root string) []lintIssue {
	files, err := collectGoFiles(root)
	if err != nil {
		return nil
	}

	var issues []lintIssue
	for _, pkgFiles := range analyzer.GroupFilesByDir(files) {
		deadIssues := analyzer.NewDeadCodeDetector(pkgFiles).DetectDeadFunctions()
		for _, d := range deadIssues {
			target := d.Name
			if d.Receiver != "" {
				target = d.Receiver + ":" + d.Name
			}
			issues = append(issues, lintIssue{
				File:       d.File,
				Rule:       "dead-code",
				Severity:   "warning",
				Message:    d.Summary(),
				AutoFixCmd: "delete " + d.File + " " + target + " --safe",
			})
		}
	}
	return issues
}
