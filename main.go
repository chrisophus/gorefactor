package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gorefactor/analyzer"
	"gorefactor/extractor"
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
		"extract": {
			Name:        "extract",
			Description: "Extract a method from a code block",
			Run:         extractMethod,
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
		"analyze-file-sizes": {
			Name:        "analyze-file-sizes",
			Description: "Analyze Go files in a directory for size issues and extraction opportunities",
			Run:         analyzeFileSizes,
		},
		"move": {
			Name:        "move",
			Description: "Move a function or method to a different file",
			Run:         moveCode,
		},
	}
}

func main() {
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

func extractMethod(args []string) error {
	if len(args) < 4 {
		return fmt.Errorf("missing required arguments: file path, start line, end line, and method name")
	}

	filePath := args[0]
	startLine, err := strconv.Atoi(args[1])
	if err != nil {
		return fmt.Errorf("invalid start line: %v", err)
	}

	endLine, err := strconv.Atoi(args[2])
	if err != nil {
		return fmt.Errorf("invalid end line: %v", err)
	}

	methodName := args[3]

	result, err := extractor.ExtractMethod(filePath, startLine, endLine, methodName)
	if err != nil {
		return err
	}

	// Output as JSON
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
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

func analyzeFileSizes(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: analyze-file-sizes <directory> [--max-size N] [--format json|text]")
	}

	directory := args[0]
	maxSize := 300
	format := "text"

	// Parse optional arguments
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--max-size":
			if i+1 < len(args) {
				size, err := strconv.Atoi(args[i+1])
				if err != nil {
					return fmt.Errorf("invalid max-size: %w", err)
				}
				maxSize = size
				i++
			}
		case "--format":
			if i+1 < len(args) {
				format = args[i+1]
				i++
			}
		}
	}

	// Find all Go files
	files, err := findGoFiles(directory)
	if err != nil {
		return fmt.Errorf("failed to find Go files: %w", err)
	}

	if len(files) == 0 {
		fmt.Println("No Go files found in directory")
		return nil
	}

	// Analyze each file
	var issues []*analyzer.FileSizeIssue
	for _, file := range files {
		issue, err := analyzer.AnalyzeFileSize(file, maxSize)
		if err != nil {
			// Log but don't fail
			fmt.Fprintf(os.Stderr, "Warning: failed to analyze %s: %v\n", file, err)
			continue
		}
		issues = append(issues, issue)
	}

	// Output results
	if format == "json" {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(issues)
	}

	// Text format (linter-style)
	oversized := 0
	for _, issue := range issues {
		if issue.IsOversized {
			oversized++
			fmt.Printf("%s: %d lines (max: %d) - %d lines over limit\n",
				issue.FilePath, issue.LineCount, issue.MaxRecommended, issue.OverageSize)

			// Show extraction hints
			if len(issue.ExtractionHints) > 0 {
				fmt.Println("  Extraction candidates:")
				for _, hint := range issue.ExtractionHints {
					fmt.Printf("    - %s (lines %d-%d, %d lines, complexity %d, priority %d/10)\n",
						hint.FunctionName, hint.StartLine, hint.EndLine, hint.LineCount, hint.Complexity, hint.Priority)
				}
			}
		}
	}

	// Summary
	fmt.Printf("\nSummary: %d/%d files exceed %d lines\n", oversized, len(issues), maxSize)

	if oversized > 0 {
		return fmt.Errorf("found %d oversized file(s)", oversized)
	}

	return nil
}

func findGoFiles(directory string) ([]string, error) {
	var files []string
	err := filepath.Walk(directory, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(path, ".go") {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

func moveCode(args []string) error {
	if len(args) < 3 {
		return fmt.Errorf("usage: move <source-file> <target-name> <destination-file>\n\nExamples:\n  gorefactor move service.go GetUser utils.go\n  gorefactor move handler.go Handler:Process helpers.go")
	}

	sourceFile := args[0]
	targetName := args[1]
	destFile := args[2]

	// Parse target name - could be "FunctionName" or "Receiver:MethodName"
	var functionName, receiverType string
	if strings.Contains(targetName, ":") {
		parts := strings.Split(targetName, ":")
		receiverType = parts[0]
		functionName = parts[1]
	} else if strings.Contains(targetName, ".") {
		parts := strings.Split(targetName, ".")
		receiverType = parts[0]
		functionName = parts[1]
	} else {
		functionName = targetName
	}

	// Create a refactoring plan and execute it
	plan := &orchestrator.RefactoringPlan{
		Version:     "1.0",
		Name:        "move_operation",
		Description: fmt.Sprintf("Move %s to %s", targetName, destFile),
		Operations: []*orchestrator.RefactoringOperation{
			{
				Type:        "move_method",
				Description: fmt.Sprintf("Move %s from %s to %s", targetName, sourceFile, destFile),
				File:        sourceFile,
				Target: &orchestrator.TargetSpecification{
					FunctionName: functionName,
					MethodName:   functionName,
					ReceiverType: receiverType,
				},
				Parameters: map[string]interface{}{
					"newFile": destFile,
				},
			},
		},
	}

	// Execute the plan
	orch := orchestrator.NewOrchestrator()
	orch.RegisterPlan(plan)

	result, err := orch.ExecutePlan(plan.Name)
	if err != nil {
		return fmt.Errorf("failed to move code: %w", err)
	}

	// Output results
	if result.Success {
		fmt.Printf("✓ Successfully moved %s to %s\n", targetName, destFile)
		for _, change := range result.Operations[0].Changes {
			fmt.Printf("  %s: %s (lines %d-%d)\n", change.Type, change.Description, change.StartLine, change.EndLine)
		}
	} else {
		fmt.Printf("✗ Failed to move %s\n", targetName)
		for _, err := range result.Errors {
			fmt.Printf("  Error: %s\n", err)
		}
		return fmt.Errorf("move operation failed")
	}

	return nil
}
