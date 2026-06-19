package main

import (
	"fmt"
	"os"
	"strings"
)

// REPLContext holds the state of the REPL session
type REPLContext struct {
	currentFile string
	workDir     string
	history     []string
}

// NewREPLContext creates a new REPL context
func NewREPLContext() *REPLContext {
	return &REPLContext{
		workDir: ".",
		history: make([]string, 0),
	}
}

// handleCommand processes a REPL command
func (ctx *REPLContext) handleCommand(input string) error {
	parts := strings.Fields(input)
	if len(parts) == 0 {
		return nil
	}

	command := parts[0]
	args := parts[1:]

	switch command {
	case "help":
		ctx.printHelp()
	case "quit", "exit":
		fmt.Println("Goodbye!")
		os.Exit(0)
	case "set-file":
		if len(args) == 0 {
			return fmt.Errorf("usage: set-file <path>")
		}
		ctx.currentFile = args[0]
		fmt.Printf("Current file: %s\n", ctx.currentFile)
	case "show-file":
		fmt.Printf("Current file: %s\n", ctx.currentFile)
	case "analyze":
		return ctx.analyze(args)
	case "suggest":
		return ctx.suggest(args)
	case "preview":
		return ctx.preview(args)
	case "apply":
		return ctx.apply(args)
	case "history":
		ctx.printHistory()
	case "clear":
		ctx.history = make([]string, 0)
		fmt.Println("History cleared")
	default:
		return fmt.Errorf("unknown command: %s", command)
	}

	ctx.history = append(ctx.history, input)
	return nil
}

// analyze performs code analysis
func (ctx *REPLContext) analyze(args []string) error {
	if ctx.currentFile == "" {
		return fmt.Errorf("no file set; use 'set-file <path>'")
	}

	fmt.Printf("Analyzing %s...\n", ctx.currentFile)
	fmt.Println("(Analysis would extract structure, complexity, metrics)")
	return nil
}

// suggest generates refactoring suggestions
func (ctx *REPLContext) suggest(args []string) error {
	if ctx.currentFile == "" {
		return fmt.Errorf("no file set; use 'set-file <path>'")
	}

	fmt.Printf("Suggesting refactorings for %s...\n", ctx.currentFile)
	fmt.Println("(Would call suggest-plan internally)")
	return nil
}

// preview shows what changes would be made
func (ctx *REPLContext) preview(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: preview <operation>")
	}

	fmt.Printf("Previewing %s...\n", args[0])
	fmt.Println("(Would use dry-run to show changes without applying)")
	return nil
}

// apply executes a refactoring
func (ctx *REPLContext) apply(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: apply <operation>")
	}

	fmt.Printf("Applying %s...\n", args[0])
	fmt.Println("(Would execute plan and run tests if available)")
	return nil
}

// printHelp displays available commands
func (ctx *REPLContext) printHelp() {
	help := `
=== GoRefactor REPL Commands ===

File Management:
  set-file <path>    Set the current file to analyze
  show-file           Show the current file

Analysis & Suggestions:
  analyze [options]   Analyze the current file
  suggest            Suggest refactorings for the current file

Refactoring:
  preview <op>       Preview what a refactoring would change (dry-run)
  apply <op>         Execute a refactoring operation

History & Control:
  history            Show command history
  clear              Clear command history
  help               Show this help message
  quit/exit          Exit the REPL

Example workflow:
  1. set-file src/main.go
  2. analyze
  3. suggest
  4. preview extract-from-processData
  5. apply extract-from-processData
`
	fmt.Println(help)
}

// printHistory displays command history
func (ctx *REPLContext) printHistory() {
	if len(ctx.history) == 0 {
		fmt.Println("No history")
		return
	}

	fmt.Println("=== Command History ===")
	for i, cmd := range ctx.history {
		fmt.Printf("%d: %s\n", i+1, cmd)
	}
}
