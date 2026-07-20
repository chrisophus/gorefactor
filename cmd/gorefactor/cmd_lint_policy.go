package main

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/chrisophus/gorefactor/config"
)

var packageFromCouplingMessage = regexp.MustCompile(`^package ([^\s]+) has fan-`)

func filterIssuesByLintPolicy(issues []lintIssue, ctx LintContext) []lintIssue {
	if ctx.Config == nil {
		return issues
	}
	testRules := ctx.Config.ExcludeTestFileRuleSet()
	if len(testRules) == 0 && len(ctx.Config.Lint.ExcludePackages) == 0 {
		return issues
	}
	out := make([]lintIssue, 0, len(issues))
	for _, iss := range issues {
		if issueExcludedByLintPolicy(iss, ctx, testRules) {
			continue
		}
		out = append(out, iss)
	}
	return out
}

func issueExcludedByLintPolicy(iss lintIssue, ctx LintContext, testRules map[string]struct{}) bool {
	if ctx.Config == nil {
		return false
	}
	if _, ok := testRules[iss.Rule]; ok && isTestFile(iss.File) {
		return true
	}
	excluded := ctx.Config.ExcludedPackageSet(iss.Rule)
	if len(excluded) == 0 {
		return false
	}
	pkg := packagePathForIssue(iss, ctx.Root)
	if pkg == "" {
		return false
	}
	_, ok := excluded[config.NormalizeRepoRelativePath(pkg)]
	return ok
}

func packagePathForIssue(iss lintIssue, root string) string {
	if m := packageFromCouplingMessage.FindStringSubmatch(iss.Message); len(m) == 2 {
		return m[1]
	}
	if root == "" {
		return ""
	}
	rel, err := filepath.Rel(root, iss.File)
	if err != nil {
		return ""
	}
	rel = config.NormalizeRepoRelativePath(rel)
	if rel == "." || strings.HasPrefix(rel, "..") {
		return ""
	}
	return filepath.ToSlash(filepath.Dir(rel))
}
