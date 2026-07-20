package orchestrator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// JournalFile records one file touched by a journaled operation.
// Snapshot is the file name inside the operation's snapshot directory
// holding the pre-mutation content; Created marks files the operation
// created (undo removes them instead of restoring content).
type JournalFile struct {
	Path     string `json:"path"`
	Snapshot string `json:"snapshot,omitempty"`
	Created  bool   `json:"created,omitempty"`
}

// JournalEntry is one mutation recorded in .gorefactor/journal.json.
type JournalEntry struct {
	ID        string        `json:"id"`
	Command   string        `json:"command"`
	Detail    string        `json:"detail,omitempty"`
	Timestamp time.Time     `json:"timestamp"`
	Files     []JournalFile `json:"files"`
}

// LoadJournal returns all journaled operations, oldest first.
func LoadJournal() ([]JournalEntry, error) {
	data, err := os.ReadFile(journalFilePath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read file: %w", err)
	}
	var entries []JournalEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("corrupt journal %s: %w", journalFilePath(), err)
	}
	return entries, nil
}

// RecordOperation snapshots the pre-mutation content of changed files and
// appends an entry to the journal. before maps path -> content as it was
// before the mutation; created lists files the operation newly created.
func RecordOperation(command, detail string, before map[string][]byte, created []string) (*JournalEntry, error) {
	entries, err := LoadJournal()
	if err != nil {
		return nil, fmt.Errorf("load journal: %w", err)
	}
	journalSeq++
	entry := JournalEntry{
		ID:        fmt.Sprintf("%d-%d-%s", time.Now().UnixNano(), journalSeq, command),
		Command:   command,
		Detail:    detail,
		Timestamp: time.Now(),
	}
	snapDir := entrySnapshotDir(entry.ID)
	idx := 0
	for path, content := range before {
		if err := os.MkdirAll(snapDir, 0755); err != nil {
			return nil, fmt.Errorf("create snapshot dir: %w", err)
		}
		name := fmt.Sprintf("f%03d.snap", idx)
		idx++
		if err := os.WriteFile(filepath.Join(snapDir, name), content, 0644); err != nil {
			return nil, fmt.Errorf("write snapshot for %s: %w", path, err)
		}
		entry.Files = append(entry.Files, JournalFile{Path: path, Snapshot: name})
	}
	for _, path := range created {
		entry.Files = append(entry.Files, JournalFile{Path: path, Created: true})
	}
	entries = append(entries, entry)
	if err := saveJournal(entries); err != nil {
		return nil, fmt.Errorf("save journal: %w", err)
	}
	return &entry, nil
}

// UndoLast restores exactly the most recent journaled operation and pops it
// from the journal. It returns the undone entry and the number of files
// restored or removed.
func UndoLast() (*JournalEntry, int, error) {
	entries, err := LoadJournal()
	if err != nil {
		return nil, 0, fmt.Errorf("load journal: %w", err)
	}
	if len(entries) == 0 {
		return nil, 0, fmt.Errorf("undo journal is empty — nothing to undo")
	}
	entry := entries[len(entries)-1]
	snapDir := entrySnapshotDir(entry.ID)
	count := 0
	for _, f := range entry.Files {
		if f.Created {
			if err := os.Remove(f.Path); err != nil && !os.IsNotExist(err) {
				return nil, count, fmt.Errorf("undo: remove created file %s: %w", f.Path, err)
			}
			count++
			continue
		}
		data, err := os.ReadFile(filepath.Join(snapDir, f.Snapshot))
		if err != nil {
			return nil, count, fmt.Errorf("undo: read snapshot for %s: %w", f.Path, err)
		}
		if dir := filepath.Dir(f.Path); dir != "." && dir != "" {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return nil, count, fmt.Errorf("mkdir all: %w", err)
			}
		}
		if err := os.WriteFile(f.Path, data, 0644); err != nil {
			return nil, count, fmt.Errorf("undo: restore %s: %w", f.Path, err)
		}
		count++
	}
	_ = os.RemoveAll(snapDir)
	if err := saveJournal(entries[:len(entries)-1]); err != nil {
		return nil, count, fmt.Errorf("save journal: %w", err)
	}
	return &entry, count, nil
}

// DropJournalEntry removes an entry (and its snapshots) without restoring
// files. Used when the caller has already rolled back the operation.
func DropJournalEntry(id string) error {
	entries, err := LoadJournal()
	if err != nil {
		return fmt.Errorf("load journal: %w", err)
	}
	out := entries[:0]
	for _, e := range entries {
		if e.ID == id {
			_ = os.RemoveAll(entrySnapshotDir(e.ID))
			continue
		}
		out = append(out, e)
	}
	return saveJournal(out)
}

var journalSeq int

func journalFilePath() string {
	return filepath.Join(".gorefactor", "journal.json")
}

func entrySnapshotDir(id string) string {
	return filepath.Join(".gorefactor", "snapshots", id)
}

func saveJournal(entries []JournalEntry) error {
	if err := os.MkdirAll(filepath.Dir(journalFilePath()), 0755); err != nil {
		return fmt.Errorf("mkdir all: %w", err)
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal indent: %w", err)
	}
	return os.WriteFile(journalFilePath(), data, 0644)
}
