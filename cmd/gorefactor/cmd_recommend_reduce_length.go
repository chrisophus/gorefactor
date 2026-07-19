package main

import (
	"fmt"

	"github.com/chrisophus/gorefactor/analyzer"
)

// runReduceLength implements `gorefactor recommend --reduce-length <file>
// <Func|Receiver:Method> [--max-lines N] [--json] [--apply [--allow-returns]]`
// — the line-count analog of --reduce-complexity. It finds the minimum set of
// top-level blocks whose extraction brings an over-threshold function under
// the line limit, and with --apply extracts them.
func runReduceLength(args []string) error {
	rf, err := parseReduceFlags(args, "--reduce-length", "--max-lines", longFunctionThreshold)
	if err != nil {
		return err
	}
	if len(rf.positionals) < 2 {
		return fmt.Errorf("usage: recommend --reduce-length <file> <Func|Receiver:Method> [--max-lines N] [--json] [--apply [--allow-returns]]")
	}
	file, function := rf.positionals[0], rf.positionals[1]
	maxLines := rf.numeric

	if rf.apply {
		applied, err := reduceLengthByExtraction(file, function, maxLines, rf.allowReturns)
		if err != nil {
			return err
		}
		if applied == 0 {
			fmt.Printf("No blocks extracted from %s — no top-level block the extract engine can lift.\n", function)
			return nil
		}
		fmt.Printf("Extracted %d block(s) from %s to reduce its length.\n", applied, function)
		return nil
	}

	res, err := analyzer.RecommendLengthReduction(file, function, maxLines)
	if err != nil {
		return err
	}
	if rf.jsonOut {
		return printJSON(res)
	}

	if len(res.Extractions) == 0 {
		fmt.Printf("%s is %d lines (threshold %d) — nothing to extract.\n", res.Function, res.Lines, res.Threshold)
		return nil
	}
	fmt.Printf("%s is %d lines (threshold %d). Suggested extractions (projected %d lines):\n",
		res.Function, res.Lines, res.Threshold, res.Projected)
	for _, e := range res.Extractions {
		fmt.Printf("  lines %d-%d (sheds %d) -> %s\n", e.StartLine, e.EndLine, e.Lines, e.Suggestion)
	}
	fmt.Printf("Apply with: gorefactor recommend --reduce-length %s %s --apply\n", file, function)
	return nil

}

// reduceLengthByExtraction applies the extractions recommended for function in
// file and returns how many blocks were successfully extracted. Blocks are
// applied bottom-up so an earlier extraction never invalidates the line
// numbers of a block still queued. It is shared by the long-function and
// extract-candidate autofixes and `recommend --reduce-length --apply`.
func reduceLengthByExtraction(file, function string, maxLines int, allowReturns bool) (int, error) {
	res, err := analyzer.RecommendLengthReduction(file, function, maxLines)
	return applyRecommendedExtractions(file, res.Extractions, err, allowReturns, func(e analyzer.LengthExtraction) extractionSpec {
		return extractionSpec{StartLine: e.StartLine, EndLine: e.EndLine, Suggestion: e.Suggestion}
	})

}
