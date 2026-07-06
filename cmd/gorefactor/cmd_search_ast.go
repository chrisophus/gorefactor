package main

import (
	"fmt"

	"github.com/chrisophus/gorefactor/analyzer"
)

var searchASTFlags = map[string]bool{"--json": false, "--in": true}

func init() {
	registerCommand(Command{
		Name:        "search-ast",
		Description: "Structural search: match a Go statement/expression pattern, $_ is a wildcard [--in path] [--json]",
		Usage:       "search-ast '<pattern>' [--in path] [--json]",
		MinArgs:     1,
		MaxArgs:     1,
		Flags:       searchASTFlags,
		Run:         searchASTCommand,
	})
}

func searchASTCommand(args []string) error {
	positional, flags := parseFlags(args, searchASTFlags)
	pattern := positional[0]
	root := "."
	if flags["--in"] != "" {
		root = flags["--in"]
	}

	matches, err := analyzer.SearchASTInDir(root, pattern)
	if err != nil {
		return parseErrorf("%v", err)
	}

	if flags["--json"] != "" {
		emitJSON(map[string]interface{}{
			"pattern": pattern,
			"matches": matches,
			"total":   len(matches),
		})
		return nil
	}
	for _, m := range matches {
		fmt.Printf("%s:%d  %s\n", m.File, m.Line, m.Snippet)
	}
	fmt.Printf("%d match(es)\n", len(matches))
	return nil
}
