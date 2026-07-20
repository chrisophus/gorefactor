package orchestrator

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBatch_RecordBeforeState(t *testing.T) {
	t.Parallel()
	b := newBatch()

	b.record(map[string][]byte{
		"a.go": []byte("content-a"),
		"b.go": []byte("content-b"),
	}, nil)

	if string(b.before[canonicalPath("a.go")]) != "content-a" {
		t.Errorf("before[a.go] = %q", b.before[canonicalPath("a.go")])
	}
	if string(b.before[canonicalPath("b.go")]) != "content-b" {
		t.Errorf("before[b.go] = %q", b.before[canonicalPath("b.go")])
	}
}

func TestBatch_FirstStateWins(t *testing.T) {
	t.Parallel()
	b := newBatch()

	b.record(map[string][]byte{"a.go": []byte("original")}, nil)
	// Second record must not overwrite the first — the batch restores to the
	// pre-batch state.
	b.record(map[string][]byte{"a.go": []byte("changed")}, nil)

	if string(b.before[canonicalPath("a.go")]) != "original" {
		t.Errorf("expected first state to win, got %q", b.before[canonicalPath("a.go")])
	}
}

// TestBatch_AliasedPathsCollapse verifies the key fix behind allowing
// change-signature (absolute paths) alongside rename (relative paths) in one
// txn: the same physical file recorded under two spellings is a single batch
// entry whose first-seen pre-state wins.
func TestBatch_AliasedPathsCollapse(t *testing.T) {
	t.Parallel()
	b := newBatch()

	rel := "aliased.go"
	abs := canonicalPath(rel)
	b.record(map[string][]byte{rel: []byte("original")}, nil)
	b.record(map[string][]byte{abs: []byte("after-first-op")}, nil)

	if len(b.Touched()) != 1 {
		t.Fatalf("expected aliased paths to collapse to 1 entry, got %v", b.Touched())
	}
	if string(b.before[abs]) != "original" {
		t.Errorf("first pre-state should win across aliases, got %q", b.before[abs])
	}
}

func TestBatch_TracksCreatedFiles(t *testing.T) {
	t.Parallel()
	b := newBatch()

	b.record(nil, []string{"new.go", "another.go"})

	if !b.created[canonicalPath("new.go")] {
		t.Error("expected new.go in created set")
	}
	if !b.created[canonicalPath("another.go")] {
		t.Error("expected another.go in created set")
	}
}

func TestBatch_Touched_ReturnsSorted(t *testing.T) {
	t.Parallel()
	b := newBatch()
	b.record(map[string][]byte{
		"z.go": nil,
		"a.go": nil,
		"m.go": nil,
	}, nil)

	paths := b.Touched()
	if len(paths) != 3 {
		t.Fatalf("expected 3 paths, got %d: %v", len(paths), paths)
	}
	for i := 1; i < len(paths); i++ {
		if paths[i] < paths[i-1] {
			t.Errorf("paths not sorted: %v", paths)
		}
	}
}

func TestBatch_Rollback_WritesAndDeletes(t *testing.T) {
	// os.WriteFile and os.Remove need real paths; can't run in parallel.
	dir := t.TempDir()

	existingFile := filepath.Join(dir, "existing.go")
	if err := os.WriteFile(existingFile, []byte("new content"), 0644); err != nil {
		t.Fatal(err)
	}
	createdFile := filepath.Join(dir, "created.go")
	if err := os.WriteFile(createdFile, []byte("created"), 0644); err != nil {
		t.Fatal(err)
	}

	b := newBatch()
	b.record(map[string][]byte{existingFile: []byte("original content")}, []string{createdFile})

	b.Rollback()

	// existing.go should be restored to original content.
	got, err := os.ReadFile(existingFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "original content" {
		t.Errorf("restored content = %q, want %q", got, "original content")
	}

	// created.go should be removed.
	if _, err := os.Stat(createdFile); !os.IsNotExist(err) {
		t.Error("expected created.go to be removed by rollback")
	}
}

// TestBatch_RecordOperationFoldsIntoActiveBatch verifies the ambient wiring:
// while a batch is active, RecordOperation accumulates into it (returning no
// entry) and Commit writes exactly one journal entry.
func TestBatch_RecordOperationFoldsIntoActiveBatch(t *testing.T) {
	withWorkDir(t, t.TempDir())

	batch, err := BeginBatch()
	if err != nil {
		t.Fatalf("BeginBatch: %v", err)
	}
	defer EndBatch()

	if _, err := BeginBatch(); err == nil {
		t.Error("expected error when beginning a nested batch")
	}

	entry, err := RecordOperation("op1", "d1", map[string][]byte{"a.go": []byte("a")}, nil)
	if err != nil {
		t.Fatalf("RecordOperation: %v", err)
	}
	if entry != nil {
		t.Error("expected nil entry while batching")
	}
	entries, _ := LoadJournal()
	if len(entries) != 0 {
		t.Fatalf("expected no journal entries mid-batch, got %d", len(entries))
	}

	committed, err := batch.Commit("txn", "one unit")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if committed == nil {
		t.Fatal("expected a committed journal entry")
	}
	entries, _ = LoadJournal()
	if len(entries) != 1 {
		t.Fatalf("expected exactly 1 journal entry after commit, got %d", len(entries))
	}
}
