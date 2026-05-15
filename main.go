package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"gorefactor/analyzer"
	"gorefactor/orchestrator"
	"gorefactor/parser"
)

type Command struct {
	Name        string
	Description string
	Run         func(args []string) error
}

func getCommands() map[string]Command {
	return map[string]Command{
		"parse": {
			Name:        "parse",
			Description: "Parse a Go file and output its structure",
			Run:         parseFile,
		},
		"list-functions": {
			Name:        "list-functions",
			Description: "List all functions in a Go file",
			Run:         listFunctions,
		},
		"recommend": {
			Name:        "recommend",
			Description: "Recommend code blocks for method extraction",
			Run:         recommendExtractions,
		},
		"orchestrate": {
			Name:        "orchestrate",
			Description: "Execute refactoring operations from JSON plan files",
			Run:         orchestrateRefactoring,
		},
		"generate-templates": {
			Name:        "generate-templates",
			Description: "Generate JSON template files for refactoring plans",
			Run:         generateTemplates,
		},
		"analyze-diff": {
			Name:        "analyze-diff",
			Description: "Analyze a diff file and generate a refactoring plan",
			Run:         analyzeDiff,
		},
		"exec": {
			Name:        "exec",
			Description: "Execute a single operation from inline JSON or stdin (supports piping)",
			Run:         execOperation,
		},
	}
}

func main() {
	if len(os.Args) >= 2 && os.Args[1] == "undo" {
		if err := undoRefactoring(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmdName := os.Args[1]
	commands := getCommands()
	cmd, exists := commands[cmdName]
	if !exists {
		fmt.Printf("Unknown command: %s\n", cmdName)
		printUsage()
		os.Exit(1)
	}

	if err := cmd.Run(os.Args[2:]); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("Usage: gorefactor <command> [arguments]")
	fmt.Println("\nCommands:")
	commands := getCommands()
	for _, cmd := range commands {
		fmt.Printf("  %-15s %s\n", cmd.Name, cmd.Description)
	}
	fmt.Println("\nRecommendation Options:")
	fmt.Println("  --min-complexity N     Minimum complexity required (default: 1)")
	fmt.Println("  --max-complexity N     Maximum complexity allowed (default: 10)")
	fmt.Println("  --max-read-vars N      Maximum number of read variables (default: 20)")
	fmt.Println("  --max-write-vars N     Maximum number of write variables (default: 10)")
	fmt.Println("  --min-statements N     Minimum number of statements (default: 3)")
	fmt.Println("  --max-statements N     Maximum number of statements (default: 50)")
	fmt.Println("  --num-leading-stmts N  Number of leading statements to include (default: 1, 0 for none)")
	fmt.Println("  --function NAME        Analyze only the specified function")
}

func parseFile(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("missing file path")
	}

	info, err := parser.ParseFile(args[0])
	if err != nil {
		return err
	}

	// Output as JSON
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(info)
}

func listFunctions(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("missing file path")
	}

	info, err := parser.ParseFile(args[0])
	if err != nil {
		return err
	}

	// Output functions and methods
	fmt.Println("Functions:")
	for _, fn := range info.Functions {
		fmt.Printf("  %s\n", fn.Name)
	}

	fmt.Println("\nMethods:")
	for _, m := range info.Methods {
		fmt.Printf("  %s.%s\n", m.Receiver, m.Name)
	}

	return nil
}

func generateTemplates(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("missing output directory")
	}

	outputDir := args[0]

	// Create template generator
	tg := orchestrator.NewTemplateGenerator()

	// Generate all templates
	if err := tg.GenerateAllTemplates(outputDir); err != nil {
		return fmt.Errorf("failed to generate templates: %w", err)
	}

	fmt.Printf("Templates generated successfully in: %s\n", outputDir)
	fmt.Println("\nAvailable templates:")
	tg.PrintTemplateHelp()

	return nil
}

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

func orchestrateRefactoring(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("missing plan file path")
	}

	planFile := args[0]
	outputFile := ""
	if len(args) > 1 {
		outputFile = args[1]
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
func undoRefactoring(args []string) error {
	var snapshotDir string
	if len(args) == 0 {
		snapshots, err := orchestrator.ListSnapshots()
		if err != nil {
			return fmt.Errorf("failed to list snapshots: %w", err)
		}
		if len(snapshots) == 0 {
			return fmt.Errorf("no snapshots found in .gorefactor/snapshots/")
		}
		snapshotDir = snapshots[len(snapshots)-1]
	} else {
		arg := args[0]
		if strings.HasSuffix(arg, ".json") {
			orch := orchestrator.NewOrchestrator()
			plan, err := orch.LoadPlan(arg)
			if err != nil {
				return fmt.Errorf("failed to load plan: %w", err)
			}
			snapshotDir = orchestrator.SnapshotDir(plan.Name)
		} else if info, err := os.Stat(arg); err == nil && info.IsDir() {
			snapshotDir = arg
		} else {
			snapshotDir = orchestrator.SnapshotDir(arg)
		}
	}
	if _, err := os.Stat(snapshotDir); err != nil {
		return fmt.Errorf("snapshot not found: %s (run orchestrate first to create one)", snapshotDir)
	}
	fmt.Printf("Restoring from snapshot: %s\n", snapshotDir)
	count, err := orchestrator.RestoreSnapshot(snapshotDir)
	if err != nil {
		return fmt.Errorf("restore failed: %w", err)
	}
	fmt.Printf("Restored %d file(s).\n", count)
	return nil
}
func execOperation(args []string) error {
	var data []byte
	var err error

	if len(args) == 0 || args[0] == "-" || args[0] == "-stdin" {
		data, err = io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("failed to read stdin: %w", err)
		}
	} else {
		data = []byte(args[0])
	}

	trimmed := bytes.TrimSpace(data)
	var ops []*orchestrator.RefactoringOperation
	if len(trimmed) > 0 && trimmed[0] == '[' {
		if err := json.Unmarshal(data, &ops); err != nil {
			return fmt.Errorf("failed to parse operations: %w", err)
		}
	} else {
		var op orchestrator.RefactoringOperation
		if err := json.Unmarshal(data, &op); err != nil {
			return fmt.Errorf("failed to parse operation: %w", err)
		}
		ops = []*orchestrator.RefactoringOperation{&op}
	}

	orch := orchestrator.NewOrchestrator()
	result, err := orch.ExecuteOperations(ops)
	if err != nil {
		return err
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if encErr := encoder.Encode(result); encErr != nil {
		return encErr
	}
	if !result.Success {
		return fmt.Errorf("one or more operations failed")
	}
	return nil
}
