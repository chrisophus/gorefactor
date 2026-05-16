package main

import (
	"fmt"
	"strings"
)

// calculatePriority calculates extraction priority (1-10, higher is better)
func calculatePriority(rec ExtractionRecommendation) int {
	if !rec.Extractable {
		return 0
	}

	priority := 5 // Base priority

	// Prefer blocks with moderate complexity (sweet spot for readability)
	if rec.Complexity >= 3 && rec.Complexity <= 10 {
		priority += 2
	}

	// Prefer blocks with clear inputs/outputs
	if len(rec.ReadVars) > 0 && len(rec.WriteVars) > 0 {
		priority += 1
	}

	// Prefer shorter blocks (easier to test and maintain)
	if rec.StatementCount <= 20 {
		priority += 1
	}

	// Penalize very simple blocks
	if rec.Complexity < 2 {
		priority -= 3
	}

	// Cap at 10
	if priority > 10 {
		priority = 10
	}
	if priority < 1 {
		priority = 1
	}

	return priority
}

// suggestMethodName suggests a method name based on the block characteristics
func suggestMethodName(rec ExtractionRecommendation) string {
	// Use write variables as hints
	if len(rec.WriteVars) > 0 {
		varName := rec.WriteVars[0]
		// Capitalize and add action
		return fmt.Sprintf("calculate%s", strings.ToUpper(varName[:1])+varName[1:])
	}

	// Use read variables as hints
	if len(rec.ReadVars) > 0 {
		varName := rec.ReadVars[0]
		return fmt.Sprintf("validate%s", strings.ToUpper(varName[:1])+varName[1:])
	}

	// Default names based on complexity
	if rec.Complexity > 7 {
		return "refactor"
	}
	return "extract"
}
