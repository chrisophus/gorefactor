package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"

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
	threshold := defaultComplexityThreshold
	jsonOut := false
	var positionals []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--reduce-complexity":
			// mode flag, consume nothing
		case "--json":
			jsonOut = true
		case "--function":
			if i+1 < len(args) {
				positionals = append(positionals, args[i+1])
				i++
			}
		case "--threshold":
			if i+1 >= len(args) {
				return fmt.Errorf("missing value for --threshold")
			}
			val, err := strconv.Atoi(args[i+1])
			if err != nil {
				return fmt.Errorf("invalid value for --threshold: %v", err)
			}
			threshold = val
			i++
		default:
			positionals = append(positionals, args[i])
		}
	}
	if len(positionals) < 2 {
		return fmt.Errorf("usage: recommend --reduce-complexity <file> <Func> [--threshold N] [--json]")
	}
	file, function := positionals[0], positionals[1]

	res, err := analyzer.RecommendComplexityReduction(file, function, threshold)
	if err != nil {
		return err
	}

	if jsonOut {
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
