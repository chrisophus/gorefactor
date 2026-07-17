package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/chrisophus/gorefactor/orchestrator"
	"github.com/chrisophus/gorefactor/parser"
	"github.com/chrisophus/gorefactor/version"
)

func init() {
	registerCommand(Command{
		Name:        "parse",
		Description: "Parse a Go file and output its structure",
		Usage:       "parse <file.go>",
		MinArgs:     1,
		MaxArgs:     1,
		Run:         parseFile,
	})
	registerCommand(Command{
		Name:        "list-functions",
		Description: "List all functions in a Go file",
		Usage:       "list-functions <file.go>",
		MinArgs:     1,
		MaxArgs:     1,
		Run:         listFunctions,
	})
	registerCommand(Command{
		Name:        "generate-templates",
		Description: "Generate JSON template files for refactoring plans",
		Usage:       "generate-templates <output-dir>",
		MinArgs:     1,
		MaxArgs:     1,
		Run:         generateTemplates,
	})
}

func main() {
	os.Exit(runMain(os.Args[1:]))
}

func runMain(argv []string) int {
	if len(argv) >= 1 && (argv[0] == "-version" || argv[0] == "--version" || argv[0] == "version") {
		fmt.Println(version.Version())
		return exitOK
	}

	if len(argv) < 1 {
		printUsage()
		return exitUsage
	}

	// gorefactor help <cmd>  or  gorefactor <cmd> help
	if argv[0] == "help" && len(argv) >= 2 {
		return printCommandHelp(argv[1])
	}
	if len(argv) >= 2 && argv[1] == "help" {
		return printCommandHelp(argv[0])
	}

	cmdName := argv[0]
	cmd, exists := getCommands()[cmdName]
	if !exists {
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", cmdName)
		if hint := closestMatch(cmdName, commandNames()); hint != "" {
			fmt.Fprintf(os.Stderr, "Did you mean %q?\n", hint)
		}
		printUsage()
		return exitUsage
	}

	args := argv[1:]
	if err := checkCommandArgs(cmd, args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitCodeFor(err)
	}

	if err := cmd.Run(args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitCodeFor(err)
	}
	return exitOK
}
func generateTemplates(args []string) error {
	if len(args) < 1 {
		return usageErrorf("missing output directory")
	}

	outputDir := args[0]

	tg := orchestrator.NewTemplateGenerator()

	if err := tg.GenerateAllTemplates(outputDir); err != nil {
		return fmt.Errorf("failed to generate templates: %w", err)
	}

	fmt.Printf("Templates generated successfully in: %s\n", outputDir)
	fmt.Println("\nAvailable templates:")
	tg.PrintTemplateHelp()

	return nil
}

func listFunctions(args []string) error {
	if len(args) < 1 {
		return usageErrorf("missing file path")
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

func parseFile(args []string) error {
	if len(args) < 1 {
		return usageErrorf("missing file path")
	}

	info, err := parser.ParseFile(args[0])
	if err != nil {
		return err
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(info)
}

// Output as JSON

// Create template generator

// Generate all templates
