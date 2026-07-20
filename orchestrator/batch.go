package orchestrator

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// BeginBatch starts journal batch mode and returns the active batch. It errors
// if a batch is already active — batches do not nest. Callers must pair it with
// EndBatch (typically deferred).
func BeginBatch() (*Batch, error) {
	if activeBatch != nil {
		return nil, fmt.Errorf("a journal batch is already active")
	}
	activeBatch = newBatch()
	return activeBatch, nil
}

// Batch is the journal's transaction mode. It accumulates the pre-mutation
// state of every file touched across a group of operations so the whole group
// commits as one journal entry and rolls back as one unit. While a batch is
// active (BeginBatch/EndBatch), RecordOperation folds each operation into the
// batch instead of writing a per-operation journal entry — this is how `txn`
// turns many mutation commands into a single undo unit without a second
// snapshot system.
type Batch struct {
	before  map[string][]byte
	created map[string]bool
	seen    map[string]bool
}

// Touched returns every path the batch modified or created, sorted.
func (b *Batch) Touched() []string {
	paths := make([]string, 0, len(b.seen))
	for p := range b.seen {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths
}

// Empty reports whether the batch captured no files.
func (b *Batch) Empty() bool { return len(b.seen) == 0 }

// Rollback restores every touched file to its pre-batch state and removes
// files the batch created.
func (b *Batch) Rollback() {
	for path, content := range b.before {
		_ = os.WriteFile(path, content, 0644)
	}
	for path := range b.created {
		_ = os.Remove(path)
	}
}

// Commit writes the whole batch as a single journal entry. It writes directly
// (bypassing the active-batch redirect) so it can run while the batch is still
// active.
func (b *Batch) Commit(command, detail string) (*JournalEntry, error) {
	created := make([]string, 0, len(b.created))
	for p := range b.created {
		created = append(created, p)
	}
	sort.Strings(created)
	return recordEntry(command, detail, b.before, created)
}

// record folds one operation's pre-state into the batch. The first recorded
// state of each path wins — that is the state the batch restores to.
func (b *Batch) record(before map[string][]byte, created []string) {
	for path, content := range before {
		key := canonicalPath(path)
		if !b.seen[key] {
			b.seen[key] = true
			b.before[key] = content
		}
	}
	for _, path := range created {
		key := canonicalPath(path)
		if !b.seen[key] {
			b.seen[key] = true
			b.created[key] = true
		}
	}
}

// activeBatch, when non-nil, redirects RecordOperation into the batch. It is
// owned by the orchestrator (the journal's own concern) rather than threaded
// through the CLI, so mutation commands need not know they run inside a txn.
var activeBatch *Batch

// EndBatch clears batch mode. Safe to defer and safe to call when no batch is
// active.
func EndBatch() { activeBatch = nil }

// canonicalPath resolves p to a cleaned absolute path so the same physical
// file recorded under different spellings (e.g. a relative "a.go" from one
// command and an absolute path from another) collapses to a single batch key.
// This keeps the "first pre-mutation state wins" invariant correct across
// commands that disagree on path style.
func canonicalPath(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return filepath.Clean(p)
}

func newBatch() *Batch {
	return &Batch{
		before:  map[string][]byte{},
		created: map[string]bool{},
		seen:    map[string]bool{},
	}
}
