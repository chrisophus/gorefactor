package main

import (
	"encoding/json"
	"fmt"
	"gorefactor/analyzer"
	"os"
	"strconv"
)

func recommendExtractions(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("missing file path")
	}

	// Check for help flag
	if args[0] == "--help" {
		printUsage()
		return nil
	}

	// Create default config
	config := analyzer.DefaultConfig()
	var functionName string

	// Parse optional configuration flags
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--help":
			printUsage()
			return nil
		case "--min-complexity":
			if i+1 >= len(args) {
				return fmt.Errorf("missing value for --min-complexity")
			}
			val, err := strconv.Atoi(args[i+1])
			if err != nil {
				return fmt.Errorf("invalid value for --min-complexity: %v", err)
			}
			config.MinComplexity = val
			i++
		case "--max-complexity":
			if i+1 >= len(args) {
				return fmt.Errorf("missing value for --max-complexity")
			}
			val, err := strconv.Atoi(args[i+1])
			if err != nil {
				return fmt.Errorf("invalid value for --max-complexity: %v", err)
			}
			config.MaxComplexity = val
			i++
		case "--max-read-vars":
			if i+1 >= len(args) {
				return fmt.Errorf("missing value for --max-read-vars")
			}
			val, err := strconv.Atoi(args[i+1])
			if err != nil {
				return fmt.Errorf("invalid value for --max-read-vars: %v", err)
			}
			config.MaxReadVars = val
			i++
		case "--max-write-vars":
			if i+1 >= len(args) {
				return fmt.Errorf("missing value for --max-write-vars")
			}
			val, err := strconv.Atoi(args[i+1])
			if err != nil {
				return fmt.Errorf("invalid value for --max-write-vars: %v", err)
			}
			config.MaxWriteVars = val
			i++
		case "--min-statements":
			if i+1 >= len(args) {
				return fmt.Errorf("missing value for --min-statements")
			}
			val, err := strconv.Atoi(args[i+1])
			if err != nil {
				return fmt.Errorf("invalid value for --min-statements: %v", err)
			}
			config.MinStatements = val
			i++
		case "--max-statements":
			if i+1 >= len(args) {
				return fmt.Errorf("missing value for --max-statements")
			}
			val, err := strconv.Atoi(args[i+1])
			if err != nil {
				return fmt.Errorf("invalid value for --max-statements: %v", err)
			}
			config.MaxStatements = val
			i++
		case "--num-leading-stmts":
			if i+1 >= len(args) {
				return fmt.Errorf("missing value for --num-leading-stmts")
			}
			val, err := strconv.Atoi(args[i+1])
			if err != nil {
				return fmt.Errorf("invalid value for --num-leading-stmts: %v", err)
			}
			config.NumLeadingStmts = val
			i++
		case "--function":
			if i+1 >= len(args) {
				return fmt.Errorf("missing value for --function")
			}
			functionName = args[i+1]
			i++
		}
	}

	recommendations, err := analyzer.RecommendExtractions(args[0], functionName, config)
	if err != nil {
		return err
	}

	// Output as JSON
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(recommendations)
}
