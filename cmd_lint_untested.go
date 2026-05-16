package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// checkUntestedPackages walks the tree once and flags directories that
// contain regular .go files but no *_test.go files. golangci-lint does
// not have a per-package "tests exist" check.
func checkUntestedPackages(root string) []lintIssue {
	hasGo := map[string]bool{}
	hasTest := map[string]bool{}
	_ = filepath.Walk(root, func(path string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			if fi != nil && fi.IsDir() {
				name := fi.Name()
				if name == "vendor" || name == ".git" || name == ".gorefactor" || name == "node_modules" {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		dir := filepath.Dir(path)
		if strings.HasSuffix(path, "_test.go") {
			hasTest[dir] = true
		} else {
			hasGo[dir] = true
		}
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
