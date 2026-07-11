package main

import (
	"fmt"
	"go/format"
	"os"

	"github.com/chrisophus/gorefactor/analyzer"
	"github.com/chrisophus/gorefactor/orchestrator"
)

var removeLogReturnFlags = mutFlagSpec(map[string]bool{"--rule": true, "--aggressive": false})

var removeLogReturnRules = map[string]bool{
	"if-err-log-return":      true,
	"wrap-log-return":        true,
	"wrap-bridge-log-return": true,
}

func init() {
	registerCommand(Command{
		Name:        "remove-log-return",
		Description: "Delete redundant log statements next to error-propagating returns; wrap bare 'return err' (--aggressive also fixes non-adjacent log/return pairs)",
		Usage:       "remove-log-return <file> [--rule <if-err-log-return|wrap-log-return|wrap-bridge-log-return>] [--aggressive] [--json] [--dry-run] [--gate]",
		MinArgs:     1,
		MaxArgs:     1,
		Flags:       removeLogReturnFlags,
		Run:         removeLogReturnCommand,
	})
}

func removeLogReturnCommand(args []string) error {
	pos, flags := parseFlags(args, removeLogReturnFlags)
	if len(pos) != 1 {
		return usageErrorf("usage: remove-log-return <file> [--rule <name>]")
	}
	file := pos[0]
	rule := flags["--rule"]
	if rule != "" && !removeLogReturnRules[rule] {
		return usageErrorf("remove-log-return: unknown --rule %q", rule)
	}
	m := &mutation{op: "remove-log-return", file: file}
	m.setCommonFlags(flags)
	return m.run(func() (string, error) {
		return applyRemoveLogReturn(file, rule, flags["--aggressive"] != "")
	})
}

// applyRemoveLogReturn rewrites file in place and returns a human summary.
func applyRemoveLogReturn(file, rule string, aggressive bool) (string, error) {
	src, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	out, sites, err := analyzer.ApplyLogReturnFixes(file, src, rule, aggressive)
	if err != nil {
		return "", parseErrorf("%v", err)
	}
	if len(sites) == 0 {
		return "remove-log-return: nothing to fix", nil
	}
	formatted, err := format.Source(out)
	if err != nil {
		return "", parseErrorf("internal: remove-log-return produced unparsable Go: %v", err)
	}
	if err := os.WriteFile(file, formatted, 0644); err != nil {
		return "", err
	}
	// A deleted log call may leave its logger import unused; goimports
	// removes it (and adds fmt for newly wrapped returns).
	if err := orchestrator.FormatImports(file); err != nil {
		fmt.Fprintf(os.Stderr, "warning: format imports on %s: %v\n", file, err)
	}
	summary := fmt.Sprintf("remove-log-return: %d fixed", len(sites))
	for _, s := range sites {
		summary += fmt.Sprintf("\n  %s:%d (%s) [%s]", file, s.Line, s.Function, s.Rule)
	}
	return summary, nil
}
