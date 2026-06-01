package main

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/chrisophus/gorefactor/analyzer"
)

// checkUntestedPackages walks the tree once and flags directories that
// contain hand-authored .go files but no *_test.go files. Generated *.gen.go
// and *_gen.go sources do not count toward "needs tests".
func checkUntestedPackages(root string, walk analyzer.WalkOptions) []lintIssue {
	hasGo := map[string]bool{}
	hasTest := map[string]bool{}
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if analyzer.ShouldSkipDir(path, walk) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		dir := filepath.Dir(path)
		if strings.HasSuffix(path, "_test.go") {
			hasTest[dir] = true
			return nil
		}
		if analyzer.ShouldSkipFile(path, walk) {
			return nil
		}
		hasGo[dir] = true
		return nil
	})
	var out []lintIssue
	for dir := range hasGo {
		if hasTest[dir] {
			continue
		}
		out = append(out, lintIssue{
			File:     dir,
			Rule:     "untested-package",
			Severity: "info",
			Message:  fmt.Sprintf("package %s has no _test.go files", dir),
		})
	}
	return out
}

type untestedPackageRule struct{}

func (untestedPackageRule) Name() string { return "untested-package" }

func (r untestedPackageRule) Run(ctx LintContext) []lintIssue {
	return checkUntestedPackages(ctx.Root, ctx.WalkOpts)
}
