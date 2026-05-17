package main

import (
	"encoding/json"
	"fmt"
	"github.com/chrisophus/gorefactor/analyzer"
	"os"
)

func analyzeDiff(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("missing diff file path")
	}

	diffPath := args[0]
	outputPath := ""
	if len(args) > 1 {
		outputPath = args[1]
	}

	// Create diff analyzer
	da := analyzer.NewDiffAnalyzer()

	// Analyze the diff
	analysis, err := da.AnalyzeDiffFile(diffPath)
	if err != nil {
		return fmt.Errorf("failed to analyze diff: %w", err)
	}

	// Output results
	fmt.Printf("Diff Analysis Summary:\n")
	fmt.Printf("%s\n", analysis.Summary)
	fmt.Printf("\nDetected Changes:\n")
	for i, change := range analysis.Changes {
		fmt.Printf("  %d. %s (confidence: %.2f)\n", i+1, change.Description, change.Confidence)
		fmt.Printf("     File: %s, Lines: %d-%d\n", change.File, change.StartLine, change.EndLine)
	}

	if analysis.Plan != nil && len(analysis.Plan.Operations) > 0 {
		fmt.Printf("\nGenerated Refactoring Plan:\n")
		fmt.Printf("  Operations: %d\n", len(analysis.Plan.Operations))
		for i, op := range analysis.Plan.Operations {
			fmt.Printf("  %d. %s (%s)\n", i+1, op.Description, op.Type)
		}
	}

	// Save plan to file if specified
	if outputPath != "" {
		// Save the generated plan as JSON
		f, err := os.Create(outputPath)
		if err != nil {
			return fmt.Errorf("failed to create output file: %w", err)
		}
		defer func() { _ = f.Close() }()
		encoder := json.NewEncoder(f)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(analysis.Plan); err != nil {
			return fmt.Errorf("failed to write plan: %w", err)
		}
		fmt.Printf("\nPlan saved to: %s\n", outputPath)
	} else {
		// Output as JSON to stdout
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(analysis)
	}

	return nil
}
