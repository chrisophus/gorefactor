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
	absSnapDir, err := filepath.Abs(snapshotDir)
	if err != nil {
		absSnapDir = snapshotDir
	}
	count := 0
	err = filepath.Walk(absSnapDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(absSnapDir, path)
		if rerr != nil {
			return rerr
		}
		// When the snapshot was created from an absolute file path the
		// snapshot entry lives at <snapDir>/<abs-path-without-leading-slash>.
		// filepath.Rel gives us back that path without the leading slash.
		// Reconstruct the absolute destination by prepending the separator.
		dest := rel
		reconstructed := string(filepath.Separator) + rel
		if filepath.IsAbs(reconstructed) {
			// The reconstructed path is a valid absolute path (on Unix, all
			// paths starting with "/" satisfy this). Use it so that files
			// snapshotted by absolute path are restored to the right place.
			dest = reconstructed
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return fmt.Errorf("read snapshot %s: %w", path, rerr)
		}
		if merr := os.MkdirAll(filepath.Dir(dest), 0755); merr != nil {
			return merr
		}
		if werr := os.WriteFile(dest, data, 0644); werr != nil {
			return fmt.Errorf("restore %s: %w", dest, werr)
		}
		count++
		return nil
	})
	if err != nil {
		return count, fmt.Errorf("restore snapshot: %w", err)
	}
	return count, nil
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
		return nil, fmt.Errorf("read dir: %w", err)
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
