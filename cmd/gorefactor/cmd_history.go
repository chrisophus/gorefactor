package main

import (
	"fmt"
	"strings"

	"github.com/chrisophus/gorefactor/orchestrator"
)

var historyFlags = map[string]bool{"--json": false}

func init() {
	registerCommand(Command{
		Name:        "history",
		ReadOnly:    true,
		Description: "List the journal of mutation operations (most recent last) [--json]",
		Usage:       "history [--json]",
		MinArgs:     0,
		MaxArgs:     0,
		Flags:       historyFlags,
		Run:         historyCommand,
	})
}

func historyCommand(args []string) error {
	_, flags := parseFlags(args, historyFlags)
	entries, err := orchestrator.LoadJournal()
	if err != nil {
		return err
	}
	if flags["--json"] != "" {
		emitEnvelope(true, "", map[string]interface{}{"entries": entries, "total": len(entries)})

		return nil
	}
	if len(entries) == 0 {
		fmt.Println("Journal is empty — no mutations recorded.")
		return nil
	}
	fmt.Printf("%d journaled operation(s) (most recent last):\n", len(entries))
	for i, e := range entries {
		var paths []string
		for _, f := range e.Files {
			if f.Created {
				paths = append(paths, f.Path+" (created)")
			} else {
				paths = append(paths, f.Path)
			}
		}
		fmt.Printf("  %2d. [%s] %-12s %s\n", i+1, e.Timestamp.Format("2006-01-02 15:04:05"), e.Command, e.Detail)
		if len(paths) > 0 {
			fmt.Printf("      files: %s\n", strings.Join(paths, ", "))
		}
	}
	return nil
}
