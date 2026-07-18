package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/chrisophus/gorefactor/analyzer"
)

// Parse options

// Create suggester

// Get suggestions

// Output as JSON array

// Human-readable output

// If patterns requested, show pattern analysis

// Offer to save as plan

// Convert suggestions to orchestration plan format

// suggestionsToOrchestrationPlan converts suggestions to an orchestration plan
func suggestionsToOrchestrationPlan(suggestions []analyzer.SuggestedPlan, filePath, outputPath string) error {
	// This would typically create a RefactoringPlan structure
	// For now, output as JSON suggestions that can be manually converted
	data, err := json.MarshalIndent(suggestions, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(outputPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write plan file: %w", err)
	}

	return nil
}
func suggestPlanCommand(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: suggest-plan <file.go> [--output plan.json] [--json] [--patterns]")
	}

	filePath := args[0]
	outputFile := ""
	jsonOutput := false
	patterns := false

	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--output":
			if i+1 < len(args) {
				outputFile = args[i+1]
				i++
			}
		case "--json":
			jsonOutput = true
		case "--patterns":
			patterns = true
		}
	}

	suggester, err := analyzer.NewPlanSuggester(filePath)
	if err != nil {
		return fmt.Errorf("failed to create suggester: %w", err)
	}

	suggestions := suggester.AllSuggestions()

	if len(suggestions) == 0 {
		fmt.Println("No refactoring suggestions found for this file.")
		return nil
	}

	if jsonOutput {
		data, err := json.MarshalIndent(suggestions, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal suggestions: %w", err)
		}

		if outputFile != "" {
			if err := os.WriteFile(outputFile, data, 0644); err != nil {
				return fmt.Errorf("failed to write output file: %w", err)
			}
			fmt.Printf("Suggestions written to: %s\n", outputFile)
		} else {
			fmt.Println(string(data))
		}
		return nil
	}

	fmt.Printf("=== Refactoring Suggestions for %s ===\n\n", filePath)
	fmt.Printf("Found %d suggestion(s):\n\n", len(suggestions))

	for i, suggestion := range suggestions {
		fmt.Printf("%d. %s\n", i+1, suggestion.Name)
		fmt.Printf("   Description: %s\n", suggestion.Description)
		fmt.Printf("   Rationale: %s\n", suggestion.Rationale)
		fmt.Printf("   Complexity: %s | Risk: %s\n", suggestion.Complexity, suggestion.SafetyRisk)
		fmt.Printf("   Operations: %d\n", len(suggestion.Operations))

		for j, op := range suggestion.Operations {
			fmt.Printf("     %d. [%s] %s (priority: %d/10)\n", j+1, op.Type, op.Description, op.Priority)
		}
		fmt.Println()
	}

	if patterns {
		fmt.Println("\n=== Architectural Pattern Analysis ===")
		pd := analyzer.NewPatternDetector(suggester.File)
		detectedPatterns := pd.DetectPatterns()

		if len(detectedPatterns) == 0 {
			fmt.Println("No architectural patterns detected.")
		} else {
			fmt.Printf("Found %d pattern(s):\n\n", len(detectedPatterns))
			for i, p := range detectedPatterns {
				fmt.Printf("%d. %s\n", i+1, p.Summary())
				fmt.Printf("   Suggestion: %s\n\n", p.Suggestion)
			}
		}
	}

	if outputFile != "" {

		if err := suggestionsToOrchestrationPlan(suggestions, filePath, outputFile); err != nil {
			return fmt.Errorf("failed to create plan file: %w", err)
		}
		fmt.Printf("\nPlan template saved to: %s\n", outputFile)
	}

	return nil
}
