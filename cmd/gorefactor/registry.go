package main

import (
	"fmt"
	"os"
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

	// --- I/O contract metadata (P2 "one I/O contract") ---
	//
	// These fields are the single source of truth for the MCP tool and txn
	// allowlists, which are derived from them (see mcpReadOnlyTools,
	// mcpWriteTools, txnSafeCommands) rather than hand-maintained in parallel
	// slices. registry_metadata_test.go pins the invariants below so a new
	// command cannot silently skip classification.

	// ReadOnly marks a sensor: its default behaviour does not modify Go source
	// on disk. (A mutating opt-in flag the MCP layer strips, e.g. lint --fix,
	// does not disqualify a command.) Exactly one of ReadOnly / Mutates must
	// be set.
	ReadOnly bool
	// Mutates marks a command whose job is to edit or create Go source (or the
	// undo/txn machinery over it). Exactly one of ReadOnly / Mutates must be set.
	Mutates bool
	// Idempotent marks a Mutates command that has no additional effect when
	// re-run with the same arguments; it drives the MCP IdempotentHint. Only
	// meaningful together with Mutates.
	Idempotent bool
	// MCPTool opts the command into the MCP server's tool surface. ReadOnly
	// tools are always registered; Mutates tools are registered only under
	// --allow-write. This replaces the hand-synced mcpReadOnlyTools /
	// mcpWriteTools slices.
	MCPTool bool
	// TxnSafe marks a Mutates command that routes through the shared mutation
	// runner and may therefore appear as a line in a `txn` script. This
	// replaces the hand-synced txnAllowedCommands map. Only meaningful together
	// with Mutates.
	TxnSafe bool
}

// commandsWhere returns the sorted names of registered commands satisfying
// pred. It is the single derivation primitive behind the MCP and txn
// allowlists, so those surfaces stay in lockstep with the per-command metadata
// instead of drifting from a hand-maintained parallel list.
func commandsWhere(pred func(Command) bool) []string {
	names := make([]string, 0, len(commandRegistry))
	for name, cmd := range commandRegistry {
		if pred(cmd) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
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

// walkArgs is the single argument tokenizer behind both checkCommandArgs and
// parseFlags, so validation and extraction can never disagree (P2 "one I/O
// contract"). It splits args into positional values and a flag map using spec
// (flag name -> takes value). In strict mode an unknown flag or a missing flag
// value is a usage error; in lenient mode unknown flags are skipped (the caller
// has already validated) and a value flag at end of args records an empty value.
func walkArgs(spec map[string]bool, strict bool, args []string) (positional []string, flags map[string]string, err error) {
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
			if strict {
				return nil, nil, usageErrorf("unknown flag %s", name)
			}
			continue
		}
		if takesValue {
			if !hasInlineValue {
				if i+1 < len(args) {
					value = args[i+1]
					i++
				} else if strict {
					return nil, nil, usageErrorf("flag %s requires a value", name)
				}
			}
			flags[name] = value
		} else {
			flags[name] = "true"
		}
	}
	return positional, flags, nil
}

// checkCommandArgs strictly validates args against the command's declared
// flags and positional bounds. Unknown flags and unexpected extra positional
// arguments are usage errors (exit code 1).
func checkCommandArgs(cmd Command, args []string) error {
	positional, _, err := walkArgs(cmd.Flags, true, args)
	if err != nil {
		return usageErrorf("%v for %s\nusage: gorefactor %s", err, cmd.Name, cmd.usageLine())
	}
	n := len(positional)
	if n < cmd.MinArgs {
		return usageErrorf("missing arguments for %s\nusage: gorefactor %s", cmd.Name, cmd.usageLine())
	}
	if cmd.MaxArgs >= 0 && n > cmd.MaxArgs {
		return usageErrorf("too many arguments for %s (got %d, max %d)\nusage: gorefactor %s",
			cmd.Name, n, cmd.MaxArgs, cmd.usageLine())
	}
	return nil
}

// isFlagToken reports whether an argument should be treated as a flag.
// A bare "-" is positional by convention (read from stdin).
func isFlagToken(a string) bool {
	return len(a) > 1 && a[0] == '-'
}

// parseFlags splits args into positional arguments and a flag map using the
// same tokenizer as checkCommandArgs (walkArgs). spec maps flag name -> takes
// value. Boolean flags are recorded with value "true". Unknown flags are
// skipped because checkCommandArgs already rejected them before the command ran.
func parseFlags(args []string, spec map[string]bool) (positional []string, flags map[string]string) {
	positional, flags, _ = walkArgs(spec, false, args)
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

// printCommandHelp prints detailed help for a single command and returns the
// appropriate exit code. Used by `gorefactor help <cmd>` and
// `gorefactor <cmd> help`.
func printCommandHelp(name string) int {
	cmd, ok := commandRegistry[name]
	if !ok {
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", name)
		if hint := closestMatch(name, commandNames()); hint != "" {
			fmt.Fprintf(os.Stderr, "Did you mean %q?\n", hint)
		}
		return exitUsage
	}
	fmt.Printf("Usage: gorefactor %s\n\n%s\n", cmd.usageLine(), cmd.Description)
	return exitOK
}

func (cmd Command) usageLine() string {
	if cmd.Usage != "" {
		return cmd.Usage
	}
	return cmd.Name
}
