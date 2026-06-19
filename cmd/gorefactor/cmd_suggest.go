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
