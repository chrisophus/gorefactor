package main

import (
	"encoding/json"
	"fmt"
	"github.com/chrisophus/gorefactor/orchestrator"
	"os"
)

func orchestrateRefactoring(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("missing plan file path")
	}

	planFile := args[0]
	outputFile := ""
	dryRun := false

	// Parse arguments
	for i := 1; i < len(args); i++ {
		if args[i] == "--dry-run" {
			dryRun = true
		} else if outputFile == "" {
			outputFile = args[i]
		}
	}

	// Create orchestrator
	orch := orchestrator.NewOrchestrator()

	// Load the plan
	plan, err := orch.LoadPlan(planFile)
	if err != nil {
		return fmt.Errorf("failed to load plan: %w", err)
	}

	fmt.Printf("Loaded plan: %s\n", plan.Name)
	fmt.Printf("Description: %s\n", plan.Description)
	fmt.Printf("Operations: %d\n", len(plan.Operations))

	if dryRun {
		fmt.Printf("\n[DRY-RUN MODE] No files will be modified\n")
		dryRunResult, err := orch.ExecutePlanDryRun(plan.Name)
		if err != nil {
			return fmt.Errorf("failed to execute dry-run: %w", err)
		}

		fmt.Println(dryRunResult.Summary)

		for i, op := range dryRunResult.Operations {
			fmt.Printf("\nOperation %d: %s\n", i+1, op.Operation.Type)
			if op.Success {
				fmt.Printf("  Status: SUCCESS\n")
				fmt.Printf("  Changes: %d file(s)\n", len(op.Changes))
				for _, change := range op.Changes {
					fmt.Printf("    - %s\n", change.File)
				}
			} else {
				fmt.Printf("  Status: FAILED\n")
				fmt.Printf("  Error: %s\n", op.Error)
			}
		}

		if outputFile != "" {
			if err := orchestrator.SaveDryRunReport(dryRunResult, outputFile); err != nil {
				return fmt.Errorf("failed to save dry-run report: %w", err)
			}
			fmt.Printf("\nDry-run report saved to: %s\n", outputFile)
		}

		return nil
	}

	// Execute the plan
	result, err := orch.ExecutePlan(plan.Name)
	if err != nil {
		return fmt.Errorf("failed to execute plan: %w", err)
	}

	// Output results
	fmt.Printf("\nExecution completed at: %s\n", result.Executed.Format("2006-01-02 15:04:05"))
	fmt.Printf("Success: %t\n", result.Success)
	fmt.Printf("Statistics:\n")
	fmt.Printf("  Total operations: %d\n", result.Statistics.TotalOperations)
	fmt.Printf("  Successful: %d\n", result.Statistics.SuccessfulOperations)
	fmt.Printf("  Failed: %d\n", result.Statistics.FailedOperations)
	fmt.Printf("  Fallback used: %d\n", result.Statistics.FallbackUsed)
	fmt.Printf("  Total changes: %d\n", result.Statistics.TotalChanges)

	if len(result.Errors) > 0 {
		fmt.Printf("\nErrors:\n")
		for _, err := range result.Errors {
			fmt.Printf("  - %s\n", err)
		}
	}

	if len(result.Warnings) > 0 {
		fmt.Printf("\nWarnings:\n")
		for _, warning := range result.Warnings {
			fmt.Printf("  - %s\n", warning)
		}
	}

	// Save result to file if specified
	if outputFile != "" {
		if err := orch.SaveResult(result, outputFile); err != nil {
			return fmt.Errorf("failed to save result: %w", err)
		}
		fmt.Printf("\nResult saved to: %s\n", outputFile)
	} else {
		// Output as JSON to stdout
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}

	return nil
}
