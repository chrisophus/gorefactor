package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/chrisophus/gorefactor/orchestrator"
)

func init() {
	registerCommand(Command{
		Name:        "undo",
		Description: "Undo the most recent journaled mutation (or restore a named plan snapshot)",
		Usage:       "undo [plan.json|snapshot-dir|plan-name]",
		MinArgs:     0,
		MaxArgs:     1,
		Run:         undoRefactoring,
	})
}

// undoRefactoring with no arguments restores exactly the most recent
// journaled operation and pops it from the journal. With an argument it
// falls back to the legacy plan-snapshot restore (plan file, snapshot
// directory, or plan name).
func undoRefactoring(args []string) error {
	if len(args) == 0 {
		entry, count, err := orchestrator.UndoLast()
		if err != nil {
			return err
		}
		fmt.Printf("Undid %s: %s\n", entry.Command, entry.Detail)
		fmt.Printf("Restored %d file(s).\n", count)
		return nil
	}

	arg := args[0]
	var snapshotDir string
	if strings.HasSuffix(arg, ".json") {
		orch := orchestrator.NewOrchestrator()
		plan, err := orch.LoadPlan(arg)
		if err != nil {
			return fmt.Errorf("failed to load plan: %w", err)
		}
		snapshotDir = orchestrator.SnapshotDir(plan.Name)
	} else if info, err := os.Stat(arg); err == nil && info.IsDir() {
		snapshotDir = arg
	} else {
		snapshotDir = orchestrator.SnapshotDir(arg)
	}
	if _, err := os.Stat(snapshotDir); err != nil {
		return fmt.Errorf("snapshot not found: %s (run orchestrate first to create one)", snapshotDir)
	}
	fmt.Printf("Restoring from snapshot: %s\n", snapshotDir)
	count, err := orchestrator.RestoreSnapshot(snapshotDir)
	if err != nil {
		return fmt.Errorf("restore failed: %w", err)
	}
	fmt.Printf("Restored %d file(s).\n", count)
	return nil
}
