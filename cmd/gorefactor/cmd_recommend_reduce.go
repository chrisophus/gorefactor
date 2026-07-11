package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/chrisophus/gorefactor/analyzer"
)

// hasFlag reports whether flag appears anywhere in args.
func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

// runReduceComplexity implements `gorefactor recommend --reduce-complexity <file>
// <Func> [--threshold N] [--json]` (improvement plan item 7). It finds the
// minimum set of extractable top-level blocks that brings an over-threshold
// function below the complexity threshold.
func runReduceComplexity(args []string) error {
	rf, err := parseReduceFlags(args, "--reduce-complexity", "--threshold", defaultComplexityThreshold)
	if err != nil {
		return err
	}
	if len(rf.positionals) < 2 {
		return fmt.Errorf("usage: recommend --reduce-complexity <file> <Func> [--threshold N] [--json]")
	}
	file, function := rf.positionals[0], rf.positionals[1]
	threshold := rf.numeric

	if rf.apply {
		applied, err := reduceComplexityByExtraction(file, function, threshold, rf.allowReturns)
		if err != nil {
			return err
		}
		if applied == 0 {
			fmt.Printf("No blocks extracted from %s — complexity is concentrated in return-bearing branches the extract engine cannot lift.\n", function)
			return nil
		}
		fmt.Printf("Extracted %d block(s) from %s to reduce complexity.\n", applied, function)
		return nil
	}

	res, err := analyzer.RecommendComplexityReduction(file, function, threshold)
	if err != nil {
		return err
	}

	if rf.jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(res)
	}

	if res.Complexity <= res.Threshold {
		fmt.Printf("%s (complexity %d, threshold %d) — already under threshold; nothing to extract.\n",
			res.Function, res.Complexity, res.Threshold)
		return nil
	}
	fmt.Printf("%s (complexity %d, threshold %d) — needs to shed %d complexity point(s).\n",
		res.Function, res.Complexity, res.Threshold, res.Complexity-res.Threshold)
	if len(res.Extractions) == 0 {
		fmt.Println("No extractable top-level blocks found — the complexity is spread across straight-line code; consider splitting the function manually.")
		return nil
	}
	fmt.Printf("Suggested extractions (reduces to complexity ~%d):\n", res.Projected)
	for i, e := range res.Extractions {
		fmt.Printf("  %d. lines %d-%d  %q  (-%d complexity, %d branch(es))\n",
			i+1, e.StartLine, e.EndLine, e.Suggestion, e.Complexity, e.Branches)
	}
	if res.Projected > res.Threshold {
		fmt.Printf("Note: even after these extractions the projected complexity (%d) stays above threshold (%d) — the remainder is straight-line or tightly coupled.\n",
			res.Projected, res.Threshold)
	}
	return nil

}
