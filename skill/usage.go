package main

import (
	"fmt"
	"os"
)

func printUsage() {
	fmt.Fprintf(os.Stderr, `GoRefactor Skill - Intelligent code refactoring

Usage: skill <command> [options]

Commands:
  analyze <file>                    Analyze file and recommend extractions
  refactor <file> [max]             Apply safe refactorings (default: 3)
  extract <file> <line1> <line2> <method>  Extract specific block
  suggest <file>                    Suggest refactorings without applying

Options:
  -gorefactor string                Path to gorefactor binary (default: ./gorefactor)
  -json                            Output as JSON (default: true)

Examples:
  skill analyze path/to/file.go
  skill refactor path/to/file.go 5
  skill extract path/to/file.go 10 25 processData

`)
}
