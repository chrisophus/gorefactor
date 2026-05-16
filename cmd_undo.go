package main

import (
	"fmt"
	"gorefactor/orchestrator"
	"os"
	"strings"
)

func undoRefactoring(args []string) error {
	var snapshotDir string
	if len(args) == 0 {
		snapshots, err := orchestrator.ListSnapshots()
		if err != nil {
			return fmt.Errorf("failed to list snapshots: %w", err)
		}
		if len(snapshots) == 0 {
			return fmt.Errorf("no snapshots found in .gorefactor/snapshots/")
		}
		snapshotDir = snapshots[len(snapshots)-1]
	} else {
		arg := args[0]
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
