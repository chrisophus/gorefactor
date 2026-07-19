package main

import (
	"fmt"

	"github.com/chrisophus/gorefactor/analyzer"
)

// recommendFilterFlags holds the extraction-filter settings parsed from
// `recommend <file> [flags]` (the non-reduce path).
type recommendFilterFlags struct {
	config       *analyzer.ExtractionConfig
	functionName string
	shortMode    bool
	showHelp     bool
}

// parseRecommendFilterFlags parses --min-complexity and related filter flags
// from args[1:]. It returns showHelp when --help appears mid-flags.
func parseRecommendFilterFlags(args []string) (recommendFilterFlags, error) {
	rf := recommendFilterFlags{config: analyzer.DefaultConfig()}
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--help":
			rf.showHelp = true
			return rf, nil
		case "--min-complexity":
			val, ni, err := parseIntFlag(args, i, "--min-complexity")
			if err != nil {
				return rf, err
			}
			rf.config.MinComplexity, i = val, ni
		case "--max-complexity":
			val, ni, err := parseIntFlag(args, i, "--max-complexity")
			if err != nil {
				return rf, err
			}
			rf.config.MaxComplexity, i = val, ni
		case "--max-read-vars":
			val, ni, err := parseIntFlag(args, i, "--max-read-vars")
			if err != nil {
				return rf, err
			}
			rf.config.MaxReadVars, i = val, ni
		case "--max-write-vars":
			val, ni, err := parseIntFlag(args, i, "--max-write-vars")
			if err != nil {
				return rf, err
			}
			rf.config.MaxWriteVars, i = val, ni
		case "--min-statements":
			val, ni, err := parseIntFlag(args, i, "--min-statements")
			if err != nil {
				return rf, err
			}
			rf.config.MinStatements, i = val, ni
		case "--max-statements":
			val, ni, err := parseIntFlag(args, i, "--max-statements")
			if err != nil {
				return rf, err
			}
			rf.config.MaxStatements, i = val, ni
		case "--num-leading-stmts":
			val, ni, err := parseIntFlag(args, i, "--num-leading-stmts")
			if err != nil {
				return rf, err
			}
			rf.config.NumLeadingStmts, i = val, ni
		case "--function":
			if i+1 >= len(args) {
				return rf, fmt.Errorf("missing value for --function")
			}
			rf.functionName = args[i+1]
			i++
		case "--short":
			rf.shortMode = true
		}
	}
	return rf, nil
}
