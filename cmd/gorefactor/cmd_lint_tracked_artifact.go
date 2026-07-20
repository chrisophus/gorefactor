package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// tracked-artifact (born from the 2026-07-19 project review's dogfooding
// scorecard) flags git-tracked files that are build artifacts: compiled
// binaries (a NUL byte in the leading bytes) and generated coverage reports.
// This repo carried two multi-megabyte Mach-O binaries and a 530KB
// coverage.html for days while lint reported "no issues" — the sensors only
// walked .go ASTs, so repo hygiene was a structural blind spot. The rule
// asks git for the tracked file list (the whole point is "tracked", which
// the filesystem alone cannot answer) and self-skips when git or a repo is
// absent, like doctor's external stages. testdata/ trees are exempt: binary
// fixtures are legitimate test inputs.

type trackedArtifactRule struct{}

func (trackedArtifactRule) Name() string { return "tracked-artifact" }

func (r trackedArtifactRule) Run(ctx LintContext) []lintIssue {
	root := ctx.Root
	if root == "" {
		root = "."
	}
	out, err := exec.Command("git", "-C", root, "ls-files", "-z").Output()
	if err != nil {
		return nil // not a git repo or no git binary: nothing to check
	}
	var issues []lintIssue
	for _, rel := range strings.Split(string(out), "\x00") {
		if rel == "" || underTestdata(rel) || trackedArtifactExempt(ctx, rel) {
			continue
		}
		full := filepath.Join(root, rel)
		info, err := os.Stat(full)
		if err != nil || info.IsDir() || info.Size() == 0 {
			continue
		}
		if isCoverageArtifact(rel) {
			issues = append(issues, lintIssue{
				File:     full,
				Rule:     "tracked-artifact",
				Severity: "warning",
				Message:  "generated coverage report is git-tracked — remove it from version control and gitignore it",
			})
			continue
		}
		if looksBinary(full) {
			issues = append(issues, lintIssue{
				File:     full,
				Rule:     "tracked-artifact",
				Severity: "warning",
				Message:  fmt.Sprintf("git-tracked binary file (%d KB) — build artifacts do not belong in version control; git rm it and extend .gitignore", info.Size()/1024),
			})
		}
	}
	return issues
}

// underTestdata reports whether a repo-relative path lies in a testdata tree,
// where binary fixtures are a legitimate, conventional test input.
func underTestdata(rel string) bool {
	rel = filepath.ToSlash(rel)
	return strings.HasPrefix(rel, "testdata/") || strings.Contains(rel, "/testdata/")
}

func trackedArtifactExempt(ctx LintContext, repoRel string) bool {
	return ctx.Config != nil && ctx.Config.TrackedArtifactAllowed(repoRel)
}

// isCoverageArtifact matches the generated coverage outputs Go tooling
// produces (go test -coverprofile, go tool cover -html).
func isCoverageArtifact(rel string) bool {
	base := filepath.Base(rel)
	return base == "coverage.html" || base == "coverage.out" ||
		strings.HasSuffix(base, ".coverprofile")
}

// looksBinary reports whether a file's leading bytes contain a NUL — the
// standard text/binary discriminator (what git itself uses). Reading 8KB
// bounds the cost on large files.
func looksBinary(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, 8192)
	n, err := f.Read(buf)
	if n <= 0 || (err != nil && n == 0) {
		return false
	}
	return bytes.IndexByte(buf[:n], 0) >= 0
}
