package main

import (
	"fmt"
	"strings"

	"github.com/chrisophus/gorefactor/analyzer"
)

// AutoFix reduces an over-threshold function's cyclomatic complexity by
// extracting the greedily-chosen set of top-level blocks that
// RecommendComplexityReduction identifies. Each block is handed to the
// AST-aware `extract` engine, which refuses any block containing a return or
// jump barrier — so this is best-effort: it applies the extractions it can and
// skips the rest (complexity concentrated in return-bearing error branches is
// not mechanically extractable). Blocks are applied bottom-up (highest start
// line first) so an earlier extraction never invalidates the line numbers of a
// block still queued. Each extractCommand writes only on full success, so a
// skipped block leaves the tree untouched; under `lint --fix --verify` the
// build+test gate still guards the net result and reverts it if it goes red.
func (r complexityRule) AutoFix(issue lintIssue, _ LintContext) error {
	file, function, ok := parseComplexityAutoFixCmd(issue.AutoFixCmd)
	if !ok {
		return fmt.Errorf("malformed complexity autofix command: %q", issue.AutoFixCmd)
	}
	applied, err := reduceComplexityByExtraction(file, function, defaultComplexityThreshold, strings.Contains(issue.AutoFixCmd, "--allow-returns"))
	if err != nil {
		return fmt.Errorf("reduce complexity by extraction: %w", err)
	}
	if applied == 0 {
		return fmt.Errorf("%s: no extractable blocks (complexity is concentrated in return-bearing branches)", function)
	}
	return nil
}

// reduceComplexityByExtraction applies the extractions recommended for function
// in file and returns how many blocks were successfully extracted. It is shared
// by the complexity autofix and `recommend --reduce-complexity --apply`.
// allowReturns forwards --allow-returns to the extract engine so aggressive
// runs can lift return-bearing blocks. (Signature edited directly:
// change-signature requires a type-checking module, which the new body
// prevents mid-edit.)
func reduceComplexityByExtraction(file, function string, threshold int, allowReturns bool) (int, error) {
	res, err := analyzer.RecommendComplexityReduction(file, function, threshold)
	if err != nil {
		return 0, err
	}
	specs := make([]extractionSpec, len(res.Extractions))
	for i, e := range res.Extractions {
		specs[i] = extractionSpec{StartLine: e.StartLine, EndLine: e.EndLine, Suggestion: e.Suggestion}
	}
	return applyExtractionsBottomUp(file, specs, allowReturns), nil

}

// parseComplexityAutoFixCmd pulls the file and function out of the autofix
// command string stored on a complexity issue
// ("gorefactor recommend --reduce-complexity <file> <func> --apply").
func parseComplexityAutoFixCmd(cmd string) (file, function string, ok bool) {
	fields := strings.Fields(cmd)
	for i, f := range fields {
		if f == "--reduce-complexity" && i+2 < len(fields) {
			return fields[i+1], fields[i+2], true
		}
	}
	return "", "", false
}
