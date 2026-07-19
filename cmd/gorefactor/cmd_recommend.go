package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/chrisophus/gorefactor/analyzer"
)

func parseIntFlag(args []string, i int, flag string) (val, next int, err error) {
	if i+1 >= len(args) {
		return 0, i, fmt.Errorf("missing value for %s", flag)
	}
	val, err = strconv.Atoi(args[i+1])
	if err != nil {
		return 0, i, fmt.Errorf("invalid value for %s: %v", flag, err)
	}
	return val, i + 1, nil

}

// recommendExtractions implements `recommend <file> [flags]`: it parses the
// extraction filter flags and prints candidate blocks as JSON (or, with
// --short, a top-3 summary). --reduce-complexity/--reduce-length dispatch to
// their threshold-driven paths.
func recommendExtractions(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("missing file path")
	}

	if args[0] == "--help" {
		printUsage()
		return nil
	}

	if hasFlag(args, "--reduce-complexity") {
		return runReduceComplexity(args)
	}
	if hasFlag(args, "--reduce-length") {
		return runReduceLength(args)
	}

	rf, err := parseRecommendFilterFlags(args)
	if err != nil {
		return err
	}
	if rf.showHelp {
		printUsage()
		return nil
	}

	recommendations, err := analyzer.RecommendExtractions(args[0], rf.functionName, rf.config)
	if err != nil {
		return err
	}

	if rf.shortMode {
		if len(recommendations) == 0 {
			fmt.Println("no extraction candidates found")
			return nil
		}
		limit := 3
		if len(recommendations) < limit {
			limit = len(recommendations)
		}
		fmt.Printf("top %d extraction candidates in %s:\n", limit, args[0])
		for i, r := range recommendations[:limit] {
			fmt.Printf("  %d. lines %d-%d  complexity=%d  stmts=%d  reads=%v  writes=%v\n",
				i+1, r.StartLine, r.EndLine, r.Complexity, r.StatementCount,
				r.ReadVars, r.WriteVars)
		}
		return nil
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(recommendations)

}
