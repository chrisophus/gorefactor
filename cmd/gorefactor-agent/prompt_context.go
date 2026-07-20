package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// fileList returns a compact newline-separated list of non-test Go file paths
// relative to dir. Used in the system prompt so the model knows valid paths
// without the token cost of a full symbol dump.
func fileList(dir string) string {
	files := goFiles(dir)
	var b strings.Builder
	for _, f := range files {
		rel, _ := filepath.Rel(dir, f)
		b.WriteString(rel)
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

func goFiles(dir string) []string {
	var files []string
	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			if d != nil && d.IsDir() {
				n := d.Name()
				// Skip VCS/deps/test fixtures and ALL dot-dirs — in
				// particular .gorefactor (gorefactor's own snapshot/
				// undo store). Targeting snapshot copies would make the
				// campaign chase its own artifacts and never converge.
				if n == "vendor" || n == "testdata" ||
					(len(n) > 1 && n[0] == '.') {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			files = append(files, path)
		}
		return nil
	})
	sort.Strings(files)
	return files
}
