package main

import (
	"fmt"
	"path/filepath"

	"github.com/chrisophus/gorefactor/analyzer"
)

// test-only-live (P3 sensor backlog) flags an exported top-level symbol that
// is referenced only from _test.go files — the production build never names
// it. Such a symbol is usually a helper or type kept exported "for testing"
// that should be unexported, moved into a _test.go file, or deleted; keeping
// it exported advertises API surface that no production caller uses. The
// finding is advisory (info): an exported symbol can legitimately be public
// API consumed by another module, which this in-module scan cannot see, so
// the rule reports a review signal rather than a gate-failing defect. There is
// no single safe autofix (unexport vs. move vs. delete is a judgement call),
// so the rule is detection-only.

type testOnlyLiveRule struct{}

func (testOnlyLiveRule) Name() string { return "test-only-live" }

func (r testOnlyLiveRule) Run(ctx LintContext) []lintIssue {
	var out []lintIssue
	for _, s := range analyzer.DetectTestOnlyLiveSymbols(ctx.Files) {
		file := s.File
		if s.Line > 0 {
			file = fmt.Sprintf("%s:%d", s.File, s.Line)
		}
		out = append(out, lintIssue{
			File:     file,
			Rule:     "test-only-live",
			Severity: "info",
			Message: fmt.Sprintf("exported %s %s is referenced only from _test.go files — unexport it, move it into a test file, or delete it (or ignore if it is intentional external API)",
				s.Kind, filepath.Base(s.File)+"."+s.Name),
		})
	}
	return out
}
