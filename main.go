package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"gorefactor/analyzer"
	"gorefactor/extractor"
	"gorefactor/parser"
)

type Command struct {
	Name        string
	Description string
	Run         func(args []string) error
}

var commands = map[string]Command{
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
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmdName := os.Args[1]
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
	for _, cmd := range commands {
		fmt.Printf("  %-15s %s\n", cmd.Name, cmd.Description)
	}
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

func recommendExtractions(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("missing file path")
	}

	recommendations, err := analyzer.RecommendExtractions(args[0])
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
