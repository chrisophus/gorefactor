package main

import (
	"fmt"
	"go/format"
	"os"

	"github.com/chrisophus/gorefactor/analyzer"
	"github.com/chrisophus/gorefactor/orchestrator"
)

var wrapSentinelsFlags = mutFlagSpec(nil)

func init() {
	registerCommand(Command{
		Name:        "wrap-sentinels",
		Mutates:     true,
		Description: "Wrap bare returns of an errors.New sentinel with fmt.Errorf(\"<context>: %w\", Sentinel)",
		Usage:       "wrap-sentinels <file> <Sentinel> [--json] [--dry-run] [--gate]",
		MinArgs:     2,
		MaxArgs:     2,
		Flags:       wrapSentinelsFlags,
		Run:         wrapSentinelsCommand,
	})
}

func wrapSentinelsCommand(args []string) error {
	pos, flags := parseFlags(args, wrapSentinelsFlags)
	if len(pos) != 2 {
		return usageErrorf("usage: wrap-sentinels <file> <Sentinel>")
	}
	file, name := pos[0], pos[1]
	m := &mutation{op: "wrap-sentinels", file: file}
	m.setCommonFlags(flags)
	return m.run(func() (string, error) {
		return applyWrapSentinels(file, name)
	})
}

// applyWrapSentinels rewrites file in place and returns a human summary.
func applyWrapSentinels(file, name string) (string, error) {
	sentinels, err := analyzer.PackageErrorSentinels(packageGoFiles(file))
	if err != nil {
		return "", err
	}
	if !sentinels[name] {
		return "", notFoundErrorf("wrap-sentinels: %s is not a package-level errors.New sentinel in %s's package", name, file)
	}
	src, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	out, n, err := analyzer.ApplySentinelWrapFixes(file, src, name)
	if err != nil {
		return "", parseErrorf("%v", err)
	}
	if n == 0 {
		return fmt.Sprintf("wrap-sentinels: no bare returns of %s in %s — nothing to fix", name, file), nil
	}
	formatted, err := format.Source(out)
	if err != nil {
		return "", parseErrorf("internal: wrap-sentinels produced unparsable Go: %v", err)
	}
	if err := os.WriteFile(file, formatted, 0644); err != nil {
		return "", err
	}
	if err := orchestrator.FormatImports(file); err != nil {
		fmt.Fprintf(os.Stderr, "warning: format imports on %s: %v\n", file, err)
	}
	return fmt.Sprintf("wrap-sentinels: wrapped %d return(s) of %s", n, name), nil
}
