package main

import (
	"fmt"
	"go/format"
	"os"

	"github.com/chrisophus/gorefactor/analyzer"
	"github.com/chrisophus/gorefactor/orchestrator"
)

var reorderFuncorderFlags = mutFlagSpec(nil)

func init() {
	registerCommand(Command{
		Name:        "reorder-funcorder",
		Description: "Reorder struct constructors/methods to satisfy funcorder placement rules (constructor after struct, exported methods before unexported)",
		Usage:       "reorder-funcorder <file> [--json] [--dry-run] [--gate]",
		MinArgs:     1,
		MaxArgs:     1,
		Flags:       reorderFuncorderFlags,
		Run:         reorderFuncorderCommand,
	})
}

func reorderFuncorderCommand(args []string) error {
	pos, flags := parseFlags(args, reorderFuncorderFlags)
	if len(pos) != 1 {
		return usageErrorf("usage: reorder-funcorder <file>")
	}
	file := pos[0]
	m := &mutation{op: "reorder-funcorder", file: file}
	m.setCommonFlags(flags)
	return m.run(func() (string, error) {
		return applyReorderFuncorder(file)
	})
}

// applyReorderFuncorder rewrites file in place and returns a human summary.
func applyReorderFuncorder(file string) (string, error) {
	src, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	out, n, err := analyzer.ApplyFuncorderFixes(file, src)
	if err != nil {
		return "", parseErrorf("%v", err)
	}
	if n == 0 {
		return fmt.Sprintf("reorder-funcorder: %s already satisfies funcorder ordering — nothing to fix", file), nil
	}
	formatted, err := format.Source(out)
	if err != nil {
		return "", parseErrorf("internal: reorder-funcorder produced unparsable Go: %v", err)
	}
	if err := os.WriteFile(file, formatted, 0644); err != nil {
		return "", err
	}
	if err := orchestrator.FormatImports(file); err != nil {
		fmt.Fprintf(os.Stderr, "warning: format imports on %s: %v\n", file, err)
	}
	return fmt.Sprintf("reorder-funcorder: reordered %d struct group(s) in %s", n, file), nil
}
