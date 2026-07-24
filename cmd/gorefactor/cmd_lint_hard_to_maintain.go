package main

import (
	"fmt"

	"github.com/chrisophus/gorefactor/analyzer"
)

// hard-to-maintain is the combined size/shape gate: it fires only when a
// function is long *and* also complex, deeply nested, or dense with early
// returns. Long-but-simple orchestrators (straight-line setup, dispatch
// tables already demoted by the single-axis rules) stay quiet. The single-axis
// proxies (long-function, complexity, deep-nesting) remain as info-tier
// sensors so agents can still see each axis; this rule is the one that fails
// the gate.

const (
	hardToMaintainLengthFloor   = analyzer.DefaultLongFunctionLines // 75
	hardToMaintainComplexityBar = defaultComplexityThreshold        // 15
	hardToMaintainNestingBar    = maxNestingThreshold               // 5
	hardToMaintainErrorPathBar  = 8
)

type hardToMaintainRule struct{}

func (hardToMaintainRule) Name() string { return "hard-to-maintain" }

func (r hardToMaintainRule) Run(ctx LintContext) []lintIssue {
	var out []lintIssue
	for _, f := range ctx.Files {
		metrics, err := analyzer.FunctionMetricsForFile(f)
		if err != nil {
			continue
		}
		lengthFloor := hardToMaintainLengthFloor
		complexityBar := hardToMaintainComplexityBar
		nestingBar := hardToMaintainNestingBar
		errorBar := hardToMaintainErrorPathBar
		if isTestFile(f) {
			lengthFloor *= longFunctionTestFactor
			complexityBar *= longFunctionTestFactor
			nestingBar++
			errorBar *= longFunctionTestFactor
		}
		for _, m := range metrics {
			lines := m.LogicLines()
			complexity := m.Complexity
			if d := m.Dispatch; d != nil {
				lines -= d.LineDiscount
				if d.NormalizedComplexity < complexity {
					complexity = d.NormalizedComplexity
				}
			}
			if lines < lengthFloor {
				continue
			}
			reasons := hardToMaintainReasons(m, complexity, complexityBar, nestingBar, errorBar)
			if len(reasons) == 0 {
				continue
			}
			sev := "warning"
			if lines >= lengthFloor*2 && (complexity > complexityBar*2 || m.MaxNesting > nestingBar+2) {
				sev = "error"
			}
			out = append(out, lintIssue{
				File:      f,
				Rule:      "hard-to-maintain",
				Severity:  sev,
				Message:   fmt.Sprintf("%s is hard to maintain (line %d): %d logic lines and %s — consider extracting", m.Key(), m.Line, lines, joinAnd(reasons)),
				Value:     lines,
				Threshold: lengthFloor,
			})
		}
	}
	return out
}

func hardToMaintainReasons(m analyzer.FunctionMetrics, complexity, complexityBar, nestingBar, errorBar int) []string {
	var reasons []string
	if complexity > complexityBar {
		reasons = append(reasons, fmt.Sprintf("complexity %d (bar %d)", complexity, complexityBar))
	}
	if m.MaxNesting > nestingBar {
		reasons = append(reasons, fmt.Sprintf("nesting %d (bar %d)", m.MaxNesting, nestingBar))
	}
	if m.ErrorPaths > errorBar {
		reasons = append(reasons, fmt.Sprintf("%d early-return paths (bar %d)", m.ErrorPaths, errorBar))
	}
	return reasons
}

func joinAnd(parts []string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	case 2:
		return parts[0] + " and " + parts[1]
	default:
		return parts[0] + ", " + joinAnd(parts[1:])
	}
}
