package main

import (
	"strings"

	"github.com/chrisophus/gorefactor/analyzer"
)

// god-package (P3 sensor backlog) is the package-level complement to the
// per-type god-object smell: it flags a package that has accreted so many
// declarations across so many files — or reached into so many sibling packages
// — that it no longer has a single responsibility. Thresholds are deliberately
// high (see analyzer.GodPackageMax*) so that large-but-cohesive packages (a
// command-per-file CLI, a big analyzer) are not flagged: a package must be
// over the declaration threshold AND either sprawling (files) or highly
// coupled (intra-module fan-out). It is a size/shape proxy — reported at
// warning severity, scored at proxy weight — and detection-only: splitting a
// package is a design decision with no single safe automatic transform.

type godPackageRule struct{}

func (godPackageRule) Name() string { return "god-package" }

func (r godPackageRule) Run(ctx LintContext) []lintIssue {
	root := ctx.Root
	if root == "" {
		root = "."
	}
	mod := moduleImportPath(root)
	var out []lintIssue
	for _, gp := range analyzer.DetectGodPackages(ctx.Files, mod) {
		out = append(out, lintIssue{
			File:     gp.Dir,
			Rule:     "god-package",
			Severity: "warning",
			Message: "god package: " + strings.Join(gp.Reasons, ", ") +
				" — split by responsibility into cohesive sub-packages",
		})
	}
	return out
}
