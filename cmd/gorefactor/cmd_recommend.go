package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/chrisophus/gorefactor/analyzer"
)

// Check for help flag

// Create default config

// Parse optional configuration flags

// Full JSON output
func parseIntFlag(args []string, i int, flag string) (val, next int, err error) {
	if i+1 >= len(args) {

		// Improvement plan item 7: complexity-threshold mode. Collect the non-flag
		// positionals (file + function) and dispatch to the reduce-complexity path.
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

	config := analyzer.DefaultConfig()
	var functionName string

	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--help":
			printUsage()
			return nil
		case "--min-complexity":
			val, ni, err := parseIntFlag(args, i, "--min-complexity")
			if err != nil {
				return err
			}
			config.MinComplexity, i = val, ni
		case "--max-complexity":
			val, ni, err := parseIntFlag(args, i, "--max-complexity")
			if err != nil {
				return err
			}
			config.MaxComplexity, i = val, ni
		case "--max-read-vars":
			val, ni, err := parseIntFlag(args, i, "--max-read-vars")
			if err != nil {
				return err
			}
			config.MaxReadVars, i = val, ni
		case "--max-write-vars":
			val, ni, err := parseIntFlag(args, i, "--max-write-vars")
			if err != nil {
				return err
			}
			config.MaxWriteVars, i = val, ni
		case "--min-statements":
			val, ni, err := parseIntFlag(args, i, "--min-statements")
			if err != nil {
				return err
			}
			config.MinStatements, i = val, ni
		case "--max-statements":
			val, ni, err := parseIntFlag(args, i, "--max-statements")
			if err != nil {
				return err
			}
			config.MaxStatements, i = val, ni
		case "--num-leading-stmts":
			val, ni, err := parseIntFlag(args, i, "--num-leading-stmts")
			if err != nil {
				return err
			}
			config.NumLeadingStmts, i = val, ni
		case "--function":
			if i+1 >= len(args) {
				return fmt.Errorf("missing value for --function")
			}
			functionName = args[i+1]
			i++
		}
	}

	shortMode := false
	for _, a := range args[1:] {
		if a == "--short" {
			shortMode = true
		}
	}

	recommendations, err := analyzer.RecommendExtractions(args[0], functionName, config)
	if err != nil {
		return err
	}

	if shortMode {
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
