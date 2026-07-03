package main

import (
	"fmt"
	"sort"
	"strings"
)

// Command describes a CLI subcommand. Command files self-register via
// registerCommand from an init() function, so adding a new command means
// adding a single new file — main.go does not need to change.
type Command struct {
	Name        string
	Description string
	// Usage is the full usage line shown on argument errors,
	// e.g. "delete <file> <Func|Receiver:Method> [--safe] [--json]".
	Usage string
	// MinArgs/MaxArgs bound the number of positional (non-flag) arguments.
	// MaxArgs of -1 means unlimited.
	MinArgs int
	MaxArgs int
	// Flags maps an accepted flag name (e.g. "--json") to whether it
	// consumes a value argument (e.g. "--max N" -> true).
	Flags map[string]bool
	Run   func(args []string) error
}

var commandRegistry = map[string]Command{}

func registerCommand(cmd Command) {
	if cmd.Name == "" || cmd.Run == nil {
		panic("registerCommand: command must have a Name and a Run function")
	}
	if _, dup := commandRegistry[cmd.Name]; dup {
		panic("registerCommand: duplicate command " + cmd.Name)
	}
	commandRegistry[cmd.Name] = cmd
}

func getCommands() map[string]Command {
	return commandRegistry
}

func commandNames() []string {
	names := make([]string, 0, len(commandRegistry))
	for name := range commandRegistry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// checkCommandArgs strictly validates args against the command's declared
// flags and positional bounds. Unknown flags and unexpected extra positional
// arguments are usage errors (exit code 1).
func checkCommandArgs(cmd Command, args []string) error {
	positional := 0
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" { // POSIX end-of-flags: everything after is positional
			positional += len(args) - i - 1
			break
		}
		if isFlagToken(a) {
			name := a
			hasInlineValue := false
			if eq := strings.Index(a, "="); eq >= 0 {
				name = a[:eq]
				hasInlineValue = true
			}
			takesValue, known := cmd.Flags[name]
			if !known {
				return usageErrorf("unknown flag %s for %s\nusage: gorefactor %s", name, cmd.Name, cmd.usageLine())
			}
			if takesValue && !hasInlineValue {
				if i+1 >= len(args) {
					return usageErrorf("flag %s requires a value\nusage: gorefactor %s", name, cmd.usageLine())
				}
				i++
			}
			continue
		}
		positional++
	}
	if positional < cmd.MinArgs {
		return usageErrorf("missing arguments for %s\nusage: gorefactor %s", cmd.Name, cmd.usageLine())
	}
	if cmd.MaxArgs >= 0 && positional > cmd.MaxArgs {
		return usageErrorf("too many arguments for %s (got %d, max %d)\nusage: gorefactor %s",
			cmd.Name, positional, cmd.MaxArgs, cmd.usageLine())
	}
	return nil
}

// isFlagToken reports whether an argument should be treated as a flag.
// A bare "-" is positional by convention (read from stdin).
func isFlagToken(a string) bool {
	return len(a) > 1 && a[0] == '-'
}

// parseFlags splits args into positional arguments and a flag map using the
// same rules as checkCommandArgs. spec maps flag name -> takes value.
// Boolean flags are recorded with value "true".
func parseFlags(args []string, spec map[string]bool) (positional []string, flags map[string]string) {
	flags = map[string]string{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" { // POSIX end-of-flags: everything after is positional
			positional = append(positional, args[i+1:]...)
			break
		}
		if !isFlagToken(a) {
			positional = append(positional, a)
			continue
		}
		name, value, hasInlineValue := strings.Cut(a, "=")
		takesValue, known := spec[name]
		if !known {
			continue // checkCommandArgs already rejected unknown flags
		}
		if takesValue {
			if !hasInlineValue && i+1 < len(args) {
				value = args[i+1]
				i++
			}
			flags[name] = value
		} else {
			flags[name] = "true"
		}
	}
	return positional, flags
}

func printUsage() {
	fmt.Println("Usage: gorefactor <command> [arguments]")
	fmt.Println("\nCommands:")
	for _, name := range commandNames() {
		cmd := commandRegistry[name]
		fmt.Printf("  %-20s %s\n", cmd.Name, cmd.Description)
	}
	fmt.Println("\nExit codes:")
	fmt.Printf("  %d  success\n", exitOK)
	fmt.Printf("  %d  usage / argument error\n", exitUsage)
	fmt.Printf("  %d  target or pattern not found (semantic miss — retry with a different target)\n", exitNotFound)
	fmt.Printf("  %d  parse / syntax rejection (snippet or file does not parse)\n", exitParseError)
	fmt.Printf("  %d  gate failure (build/test failed; used by doctor and --gate)\n", exitGateFailure)
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
func (cmd Command) usageLine() string {
	if cmd.Usage != "" {
		return cmd.Usage
	}
	return cmd.Name
}
