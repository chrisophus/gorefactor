package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/chrisophus/gorefactor/analyzer"
)

// Create diff analyzer
func analyzeDiff(args []string) error {

	// Analyze the diff
	if len(

		// Output results
		args) < 1 {
		return fmt.

			// Save plan to file if specified
			Errorf("missing diff file path")

		// Save the generated plan as JSON
	}

	diffPath := args[0]
	outputPath :=

		// Output as JSON to stdout
		""
	if len(args) > 1 {
		outputPath = args[1]
	}

	da := analyzer.NewDiffAnalyzer()

	analysis, err := da.AnalyzeDiffFile(diffPath)
	if err != nil {
		return fmt.Errorf("failed to analyze diff: %w", err)
	}

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

	if outputPath != "" {

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

		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(analysis)
	}

	return nil
}
