package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func main() {
	listFlag := flag.Bool("list", false, "List all available scenarios")
	scenarioFlag := flag.String("scenario", "", "Run a specific scenario by name")
	verboseFlag := flag.Bool("v", false, "Verbose output")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `Phase 4 Pi Integration Testing

Usage:
  gorefactor-test [options]

Options:
  -list              List all scenarios
  -scenario <name>   Run a specific scenario
  -v                 Verbose output

Examples:
  gorefactor-test -list
  gorefactor-test -scenario "Move Command - Target Not Found"
  gorefactor-test                  # Run all scenarios
`)
	}

	flag.Parse()

	if *listFlag {
		PrintScenarios()
		return
	}

	// Ensure gorefactor is built and available
	gorefactorPath, err := ensureGorefactorBinary()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Set PATH to include gorefactor directory
	if err := os.Setenv("PATH", filepath.Dir(gorefactorPath)+":"+os.Getenv("PATH")); err != nil {
		fmt.Fprintf(os.Stderr, "Error setting PATH: %v\n", err)
		os.Exit(1)
	}

	var scenarios []TestScenario
	if *scenarioFlag != "" {
		scenario := GetScenarioByName(*scenarioFlag)
		if scenario == nil {
			fmt.Fprintf(os.Stderr, "Scenario not found: %s\n", *scenarioFlag)
			PrintScenarios()
			os.Exit(1)
		}
		scenarios = []TestScenario{*scenario}
	} else {
		scenarios = AllScenarios()
	}

	// Run scenarios
	fmt.Printf("\n╔════════════════════════════════════════════════════╗\n")
	fmt.Printf("║   Phase 4: Pi Integration Testing                ║\n")
	fmt.Printf("╚════════════════════════════════════════════════════╝\n\n")

	results := runScenarios(scenarios, *verboseFlag)

	// Print summary
	fmt.Printf("\n╔════════════════════════════════════════════════════╗\n")
	fmt.Printf("║   Summary                                          ║\n")
	fmt.Printf("╚════════════════════════════════════════════════════╝\n\n")

	successCount := 0
	for _, result := range results {
		if result.Success {
			successCount++
		}
		PrintResult(result)
	}

	fmt.Printf("\nResults: %d/%d passed\n", successCount, len(results))

	if successCount == len(results) {
		fmt.Printf("\n✅ All scenarios passed!\n\n")
		os.Exit(0)
	} else {
		fmt.Printf("\n❌ Some scenarios failed\n\n")
		os.Exit(1)
	}
}

// runScenarios executes all scenarios and collects results
func runScenarios(scenarios []TestScenario, verbose bool) []TestResult {
	var results []TestResult

	for i, scenario := range scenarios {
		fmt.Printf("[%d/%d] Running: %s\n", i+1, len(scenarios), scenario.Name)
		if verbose {
			fmt.Printf("      Description: %s\n", scenario.Description)
			fmt.Printf("      Expected Error: %s\n", scenario.ExpectedErrorCode)
		}

		// Create test directory
		testDir, err := CreateTestDir()
		if err != nil {
			fmt.Printf("     Error creating test directory: %v\n", err)
			results = append(results, TestResult{
				ScenarioName: scenario.Name,
				ErrorMessage: fmt.Sprintf("Test setup failed: %v", err),
			})
			continue
		}
		defer func() { _ = os.RemoveAll(testDir) }()

		// Run scenario
		result := RunScenario(scenario, testDir)
		results = append(results, result)

		if verbose && result.Success {
			fmt.Printf("     ✅ Success\n")
			if result.ErrorCode != "" {
				fmt.Printf("     Error Code: %s\n", result.ErrorCode)
			}
			if len(result.Suggestions) > 0 {
				fmt.Printf("     Suggestions: %v\n", result.Suggestions)
			}
			fmt.Printf("     Recovery Attempts: %d/%d\n", result.RecoveryAttempt, result.TotalSteps)
		} else if verbose && !result.Success {
			fmt.Printf("     ❌ Failed: %s\n", result.ErrorMessage)
		}

		fmt.Println()
	}

	return results
}

// ensureGorefactorBinary builds gorefactor if needed and returns absolute path
func ensureGorefactorBinary() (string, error) {
	// Check if gorefactor exists in PATH
	if path, err := exec.LookPath("gorefactor"); err == nil {
		return path, nil
	}

	// Check local paths
	localPaths := []string{
		"./gorefactor",                // Current dir
		"./cmd/gorefactor/gorefactor", // Relative to repo root
	}

	for _, path := range localPaths {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			abs, _ := filepath.Abs(path)
			return abs, nil
		}
	}

	// Try to build it
	fmt.Println("Building gorefactor...")
	cwd, _ := os.Getwd()
	binaryPath := filepath.Join(cwd, "gorefactor")
	cmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/gorefactor")
	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("failed to build gorefactor: %v\n%s", err, output)
	}

	fmt.Println("✅ Built gorefactor")
	return binaryPath, nil
}
