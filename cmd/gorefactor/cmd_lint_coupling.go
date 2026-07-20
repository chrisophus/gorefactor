package main

import (
	"fmt"
	"strings"

	"github.com/chrisophus/gorefactor/analyzer"
)

const (
	defaultFanOutThreshold = 10
	defaultFanInThreshold  = 8
)

func couplingThresholds(ctx LintContext) (fanIn, fanOut int) {
	fanIn = defaultFanInThreshold
	fanOut = defaultFanOutThreshold
	if ctx.Config != nil {
		fanIn, fanOut = ctx.Config.CouplingThresholds()
	}
	return fanIn, fanOut
}

type couplingRule struct{}

func (couplingRule) Name() string { return "high-coupling" }

// isLocalImport matches a fully-qualified import path against the list
// of local packages. PackageGraph today stores PackageInfo.Path as a
// relative leaf ("analyzer") while Imports holds the fully-qualified
// path ("github.com/foo/bar/analyzer"); see task #16 for the analyzer
// fix. Suffix matching is the pragmatic stand-in.
func isLocalImport(imp string, pkgs []*analyzer.PackageInfo) *analyzer.PackageInfo {
	for _, p := range pkgs {
		if p.Path == "" {
			continue
		}
		if imp == p.Path || strings.HasSuffix(imp, "/"+p.Path) {
			return p
		}
	}
	return nil
}

func (r couplingRule) Run(ctx LintContext) []lintIssue {
	graph, err := analyzer.NewPackageGraph(ctx.Root)
	if err != nil {
		return nil
	}
	pkgs := graph.AllPackages()

	fanIn := make(map[string]int, len(pkgs))
	for _, p := range pkgs {
		for _, imp := range p.Imports {
			if target := isLocalImport(imp, pkgs); target != nil {
				fanIn[target.Path]++
			}
		}
	}

	fanInThreshold, fanOutThreshold := couplingThresholds(ctx)

	var out []lintIssue
	for _, p := range pkgs {
		fanOut := 0
		for _, imp := range p.Imports {
			if isLocalImport(imp, pkgs) != nil {
				fanOut++
			}
		}
		if fanOut > fanOutThreshold {
			out = append(out, lintIssue{
				File:     p.Dir,
				Rule:     "high-coupling",
				Severity: "warning",
				Message: fmt.Sprintf(
					"package %s has fan-out %d (threshold %d) — depends on too many local packages; consider consolidating or inverting dependencies",
					p.Path, fanOut, fanOutThreshold,
				),
			})
		}
		if fanIn[p.Path] > fanInThreshold {
			out = append(out, lintIssue{
				File:     p.Dir,
				Rule:     "high-coupling",
				Severity: "info",
				Message: fmt.Sprintf(
					"package %s has fan-in %d (threshold %d) — many local packages depend on it; high blast radius for changes",
					p.Path, fanIn[p.Path], fanInThreshold,
				),
			})
		}
	}
	return out
}
