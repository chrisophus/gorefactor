package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/chrisophus/gorefactor/analyzer"
)

var callgraphFlags = map[string]bool{
	"--json":    false,
	"--callers": false,
	"--callees": false,
	"--depth":   true,
	"--in":      true,
}

func init() {
	registerCommand(Command{
		Name:        "callgraph",
		ReadOnly:    true,
		MCPTool:     true,
		Description: "Transitive call tree for a function/method (default: callees, depth 2) [--callers] [--depth N] [--json]",
		Usage:       "callgraph <Func|Receiver:Method> [--depth N] [--callers|--callees] [--in path] [--json]",
		MinArgs:     1,
		MaxArgs:     1,
		Flags:       callgraphFlags,
		Run:         callgraphCommand,
	})
}

func callgraphCommand(args []string) error {
	positional, flags := parseFlags(args, callgraphFlags)
	target := positional[0]
	root := "."
	if flags["--in"] != "" {
		root = flags["--in"]
	}
	depth := 2
	if flags["--depth"] != "" {
		n, err := strconv.Atoi(flags["--depth"])
		if err != nil || n < 1 {
			return usageErrorf("--depth requires a positive integer")
		}
		depth = n
	}
	if flags["--callers"] != "" && flags["--callees"] != "" {
		return usageErrorf("--callers and --callees are mutually exclusive")
	}
	direction := "callees"
	if flags["--callers"] != "" {
		direction = "callers"
	}

	files, err := collectGoFiles(root, analyzer.DefaultWalkOptions())
	if err != nil {
		return err
	}
	idx, err := buildCallIndex(files)
	if err != nil {
		return err
	}

	def, err := idx.LookupTargetOrSuggest(target)
	if err != nil {
		return err
	}

	tree := idx.BuildTree(def, direction, depth, map[string]bool{def.Key(): true})

	if flags["--json"] != "" {
		emitJSON(map[string]interface{}{
			"target":    def.Key(),
			"direction": direction,
			"depth":     depth,
			"tree":      tree,
		})
		return nil
	}
	fmt.Printf("%s of %s (depth %d):\n", direction, def.Key(), depth)
	printCgNode(tree, 0)
	return nil
}

func printCgNode(n *cgNode, indent int) {
	label := n.Name
	if n.Receiver != "" {
		label = n.Receiver + ":" + n.Name
	}
	suffix := ""
	if n.Cycle {
		suffix = " [cycle]"
	}
	fmt.Printf("%s%s  %s:%d%s\n", strings.Repeat("  ", indent), label, n.File, n.Line, suffix)
	for _, c := range n.Children {
		printCgNode(c, indent+1)
	}
}
