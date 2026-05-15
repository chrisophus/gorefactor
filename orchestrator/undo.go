package orchestrator

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

func (o *Orchestrator) createSnapshot(plan *RefactoringPlan, snapshotDir string) error {
	seen := make(map[string]bool)
	for _, op := range plan.Operations {
		if op.File == "" || seen[op.File] {
			continue
		}
		seen[op.File] = true
		data, err := os.ReadFile(op.File)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("snapshot %s: %w", op.File, err)
		}
		dest := filepath.Join(snapshotDir, op.File)
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			return fmt.Errorf("create snapshot dir: %w", err)
		}
		if err := os.WriteFile(dest, data, 0644); err != nil {
			return fmt.Errorf("write snapshot %s: %w", op.File, err)
		}
	}
	return nil
}

func RestoreSnapshot(snapshotDir string) (int, error) {
	count := 0
	err := filepath.Walk(snapshotDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(snapshotDir, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read snapshot %s: %w", path, err)
		}
		if err := os.MkdirAll(filepath.Dir(rel), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(rel, data, 0644); err != nil {
			return fmt.Errorf("restore %s: %w", rel, err)
		}
		count++
		return nil
	})
	return count, err
}

func SnapshotDir(planName string) string {
	return filepath.Join(".gorefactor", "snapshots", planName)
}

func ListSnapshots() ([]string, error) {
	baseDir := filepath.Join(".gorefactor", "snapshots")
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, filepath.Join(baseDir, e.Name()))
		}
	}
	sort.Strings(dirs)
	return dirs, nil
}
