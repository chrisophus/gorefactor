package main

import (
	"fmt"
	"os"
	"sort"
	"strconv"
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
	applied, err := reduceComplexityByExtraction(file, function, defaultComplexityThreshold)
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
func reduceComplexityByExtraction(file, function string, threshold int) (int, error) {
	res, err := analyzer.RecommendComplexityReduction(file, function, threshold)
	if err != nil {
		return 0, err
	}
	// Apply bottom-up so extracting a lower block never shifts the line numbers
	// of a higher block still queued.
	ext := append([]analyzer.ComplexityExtraction(nil), res.Extractions...)
	sort.SliceStable(ext, func(i, j int) bool { return ext[i].StartLine > ext[j].StartLine })

	applied := 0
	for _, e := range ext {
		args := []string{file, strconv.Itoa(e.StartLine), strconv.Itoa(e.EndLine), e.Suggestion}
		if err := extractCommand(args); err != nil {
			fmt.Fprintf(os.Stderr, "  skip block L%d-%d: %v\n", e.StartLine, e.EndLine, err)
			continue
		}
		applied++
	}
	return applied, nil
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
