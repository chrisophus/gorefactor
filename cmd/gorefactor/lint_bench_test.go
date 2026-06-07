package main

import (
	"testing"

	"github.com/chrisophus/gorefactor/analyzer"
)

// BenchmarkLint measures the in-process lint rule phase over this repo. It
// excludes the subprocess-backed rules (golangci-lint, arch-violation,
// untested-function) so the numbers reflect the AST work being optimized.
func BenchmarkLint(b *testing.B) {
	files, err := analyzer.WalkGoFiles("../..", analyzer.DefaultWalkOptions())
	if err != nil {
		b.Fatal(err)
	}
	skip := map[string]bool{"golangci-lint": true, "arch-violation": true, "untested-function": true}
	var rules []LintRule
	for _, r := range defaultLintRules() {
		if !skip[r.Name()] {
			rules = append(rules, r)
		}
	}
	opts := lintOptions{root: "../..", failOn: "error"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx := LintContext{Root: "../..", Files: files, MaxSize: 600}
		_ = runLintRules(rules, ctx, opts)
	}
}
