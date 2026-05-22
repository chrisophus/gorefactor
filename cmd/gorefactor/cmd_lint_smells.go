package main

import (
	"fmt"
	"go/parser"
	"go/token"
	"strings"

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

type smellRule struct{}

func (smellRule) Name() string { return "smell" }

func (r smellRule) Run(ctx LintContext) []lintIssue {
	var out []lintIssue
	for _, f := range ctx.Files {
		out = append(out, checkSmells(f)...)
	}
	return out
}

type deadCodeRule struct{}

func (deadCodeRule) Name() string { return "dead-code" }

func (r deadCodeRule) Run(ctx LintContext) []lintIssue {
	return checkDeadCode(ctx.Root)
}

func (r deadCodeRule) AutoFix(issue lintIssue, ctx LintContext) error {
	parts := strings.Fields(issue.AutoFixCmd)
	if len(parts) < 3 || parts[0] != "delete" {
		return fmt.Errorf("malformed autofix command: %q", issue.AutoFixCmd)
	}
	return deleteCommand(parts[1:])
}
