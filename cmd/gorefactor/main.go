package main

import (
	"fmt"
	"os"

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
		fmt.Println(version.Version)
		return exitOK
	}

	if len(argv) < 1 {
		printUsage()
		return exitUsage
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

// Output as JSON

// Create template generator

// Generate all templates
