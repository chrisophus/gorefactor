package main

import (
	"fmt"

	"github.com/chrisophus/gorefactor/orchestrator"
)

func init() {
	registerCommand(Command{
		Name:        "undo",
		Mutates:     true,
		MCPTool:     true,
		Idempotent:  false,
		Description: "Undo the most recent journaled mutation (restores files and pops the journal entry)",
		Usage:       "undo",
		MinArgs:     0,
		MaxArgs:     0,
		Run:         undoRefactoring,
	})
}

// undoRefactoring restores exactly the most recent journaled operation and
// pops it from the journal. The journal is the single undo system: every
// mutation path (direct commands, txn batches, orchestrate plans) records into
// it, so one code path rolls all of them back.
func undoRefactoring(_ []string) error {
	entry, count, err := orchestrator.UndoLast()
	if err != nil {
		return err
	}
	fmt.Printf("Undid %s: %s\n", entry.Command, entry.Detail)
	fmt.Printf("Restored %d file(s).\n", count)
	return nil
}
