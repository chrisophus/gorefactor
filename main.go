package main

import (
	"encoding/json"
	"fmt"
	"os"

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
		"exec": {
			Name:        "exec",
			Description: "Execute a single operation from inline JSON or stdin (supports piping)",
			Run:         execOperation,
		},
		"format": {
			Name:        "format",
			Description: "Format Go files (gofmt + goimports) in-place; pass dir/file paths or default '.'",
			Run:         formatCommand,
		},
		"split": {
			Name:        "split",
			Description: "Auto-split a Go file over the line limit into multiple files [--max N] [--dry-run]",
			Run:         splitCommand,
		},
		"lint": {
			Name:        "lint",
			Description: "Run structural lints (file size, duplicates) [--fix] [--json] [--max N]",
			Run:         lintCommand,
		},
		"create": {
			Name:        "create",
			Description: "Create a new file with content from arg or stdin",
			Run:         createCommand,
		},
		"insert": {
			Name:        "insert",
			Description: "Insert code into a file at a location (at-end | at-beginning | before:Func | after:Func | inside:Func)",
			Run:         insertCommand,
		},
		"replace": {
			Name:        "replace",
			Description: "Replace a code pattern inside a function/method (AST: pattern must be a full statement)",
			Run:         replaceCommand,
		},
		"replace-text": {
			Name:        "replace-text",
			Description: "Replace literal text inside a function/method body (safe text find/replace)",
			Run:         replaceTextCommand,
		},
		"delete": {
			Name:        "delete",
			Description: "Delete a declaration (function, method, or type) from a file",
			Run:         deleteCommand,
		},
		"rename": {
			Name:        "rename",
			Description: "Rename an unexported symbol across the package",
			Run:         renameCommand,
		},
		"find-callers": {
			Name:        "find-callers",
			Description: "Find all callers of a function or method [--in path] [--json]",
			Run:         findCallersCommand,
		},
		"find-uses": {
			Name:        "find-uses",
			Description: "Find all uses of a symbol [--in path] [--json]",
			Run:         findUsesCommand,
		},
		"find-implementations": {
			Name:        "find-implementations",
			Description: "Find all types that implement an interface [--in path] [--json]",
			Run:         findImplementationsCommand,
		},
		"inspect": {
			Name:        "inspect",
			Description: "One-page summary of a Go file: decls, sizes, lint issues, extraction candidates",
			Run:         inspectCommand,
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

	fmt.Println("Functions:")
	for _, fn := range info.Functions {
		fmt.Printf("  %s (lines %d-%d, %d lines)\n", fn.Name, fn.StartLine, fn.EndLine, fn.EndLine-fn.StartLine+1)
	}

	fmt.Println("\nMethods:")
	for _, m := range info.Methods {
		fmt.Printf("  %s.%s (lines %d-%d, %d lines)\n", m.Receiver, m.Name, m.StartLine, m.EndLine, m.EndLine-m.StartLine+1)
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
