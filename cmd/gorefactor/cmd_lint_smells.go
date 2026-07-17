package main

import (
	"fmt"
	"go/parser"
	"go/token"
	"strings"

	"github.com/chrisophus/gorefactor/analyzer"
)

// checkDeadCode detects unused unexported functions/methods per Go package
// directory. Whole-tree analysis misattributes package-local symbols; one
// detector per directory matches Go's package boundary and is much faster.
func checkDeadCode(ctx LintContext) []lintIssue {
	files := ctx.Files
	if len(files) == 0 {
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
	// Aggressive level: exported top-level functions unreferenced anywhere in
	// the module. External (out-of-module) consumers are invisible to this
	// scan, which is exactly why the fix level demands the verify gate.
	if ctx.AggressiveFix() {
		for _, d := range analyzer.DetectDeadExportedFunctions(files) {
			issues = append(issues, lintIssue{
				File:       d.File,
				Rule:       "dead-code",
				Severity:   "warning",
				Message:    d.Summary(),
				AutoFix:    "delete (aggressive)",
				AutoFixCmd: "delete " + d.File + " " + d.Name + " --safe",
			})
		}
	}
	return issues

}

// smellRule is parametric so one struct handles all seven smell types.
// Each instance carries a kebab-case ruleName (the agent-visible Rule
// field) and the human-readable smellName the PatternDetector emits.
type smellRule struct {
	ruleName  string
	smellName string
}

func (r smellRule) Name() string { return r.ruleName }

// canonicalSmellSeverity maps the PatternDetector's confidence labels onto the
// linter's canonical tiers. The detector emits "low"/"medium"/"high", which are
// not lint severities; "low" in particular is a low-confidence structural smell
// (data clumps, excess params/returns) that shouldn't sit in the warning bucket
// and inflate the health score. Unknown values pass through unchanged.
func canonicalSmellSeverity(detectorSeverity string) string {
	switch detectorSeverity {
	case "low":
		return "info"
	case "medium":
		return "warning"
	case "high":
		return "error"
	default:
		return detectorSeverity
	}
}

func (r smellRule) Run(ctx LintContext) []lintIssue {
	var out []lintIssue
	for _, f := range ctx.Files {
		fset := token.NewFileSet()
		astFile, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			continue
		}
		for _, p := range analyzer.NewPatternDetector(astFile).DetectPatterns() {
			if p.Name != r.smellName {
				continue
			}
			out = append(out, lintIssue{
				File:     f,
				Rule:     r.ruleName,
				Severity: canonicalSmellSeverity(p.Severity),
				Message:  p.Description,
			})
		}
	}
	return out
}

// smellRules splits the bundled "smell" detector into seven first-class
// rules so agents can filter or address findings by specific smell type.
func smellRules() []LintRule {
	return []LintRule{
		smellRule{ruleName: "god-object", smellName: "God Object"},
		smellRule{ruleName: "excessive-params", smellName: "Excessive Parameters"},
		smellRule{ruleName: "excessive-returns", smellName: "Excessive Return Values"},
		smellRule{ruleName: "fat-interface", smellName: "Fat Interface"},
		smellRule{ruleName: "large-class", smellName: "Large Class"},
		smellRule{ruleName: "data-clumps", smellName: "Data Clumps"},
		smellRule{ruleName: "type-switch", smellName: "Type Switches"},
	}
}

type deadCodeRule struct{}

func (deadCodeRule) Name() string { return "dead-code" }

func (r deadCodeRule) Run(ctx LintContext) []lintIssue {
	return checkDeadCode(ctx)
}

func (r deadCodeRule) AutoFix(issue lintIssue, ctx LintContext) error {
	parts := strings.Fields(issue.AutoFixCmd)
	if len(parts) < 3 || parts[0] != "delete" {
		return fmt.Errorf("malformed autofix command: %q", issue.AutoFixCmd)
	}
	return deleteCommand(parts[1:])
}
