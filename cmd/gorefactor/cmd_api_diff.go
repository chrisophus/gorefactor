package main

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/chrisophus/gorefactor/analyzer"
)

var apiDiffFlags = map[string]bool{"--json": false}

func init() {
	registerCommand(Command{
		Name:        "api-diff",
		ReadOnly:    true,
		MCPTool:     true,
		Description: "Compare the exported API surface of the working tree against a git ref (default HEAD) [--json]",
		Usage:       "api-diff [git-ref] [--json]",
		MinArgs:     0,
		MaxArgs:     1,
		Flags:       apiDiffFlags,
		Run:         apiDiffCommand,
	})
}

func apiDiffCommand(args []string) error {
	positional, flags := parseFlags(args, apiDiffFlags)
	ref := "HEAD"
	if len(positional) > 0 {
		ref = positional[0]
	}

	res, err := analyzer.ComputeAPIDiff(".", ref)
	if err != nil {
		return err
	}
	if flags["--json"] != "" {
		emitJSON(res)
		return nil
	}
	printAPIDiff(res)
	return nil
}

func printAPIDiff(res *analyzer.APIDiffResult) {
	verdict := "no breaking changes"
	if res.Breaking {
		verdict = "BREAKING"
	}
	fmt.Printf("api-diff vs %s: %d added, %d removed, %d changed (%s)\n",
		res.Ref, len(res.Added), len(res.Removed), len(res.Changed), verdict)
	for _, s := range res.Added {
		fmt.Printf("+ %s\n", s)
	}
	for _, s := range res.Removed {
		fmt.Printf("- %s\n", s)
	}
	for _, c := range res.Changed {
		fmt.Printf("~ %s\n    old: %s\n    new: %s\n", c.Symbol, c.Old, c.New)
	}
}

// gitShowPrefix reports the path of the current directory relative to the git
// repository root (empty at the root). Shared with test-affected.
func gitShowPrefix() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--show-prefix").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
