package orchestrator

import (
	"os"
	"path/filepath"
	"testing"
)

// ---- LoadJournal ----

func TestLoadJournal_MissingFile(t *testing.T) {
	withWorkDir(t, t.TempDir())
	entries, err := LoadJournal()
	if err != nil {
		t.Fatalf("expected nil error on missing journal, got: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected empty entries, got %d", len(entries))
	}
}

func TestLoadJournal_CorruptJSON(t *testing.T) {
	dir := t.TempDir()
	withWorkDir(t, dir)
	if err := os.MkdirAll(".gorefactor", 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalFilePath(), []byte("not-json{{{"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadJournal()
	if err == nil {
		t.Fatal("expected error for corrupt journal")
	}
}

// ---- RecordOperation ----

func TestRecordOperation_CreatesEntryAndSnapshot(t *testing.T) {
	dir := t.TempDir()
	withWorkDir(t, dir)

	srcPath := filepath.Join(dir, "foo.go")
	origContent := []byte("package main\n")
	if err := os.WriteFile(srcPath, origContent, 0644); err != nil {
		t.Fatal(err)
	}

	entry, err := RecordOperation("insert", "added helper", map[string][]byte{srcPath: origContent}, nil)
	if err != nil {
		t.Fatalf("RecordOperation: %v", err)
	}
	if entry.Command != "insert" {
		t.Errorf("Command = %q, want %q", entry.Command, "insert")
	}
	if entry.Detail != "added helper" {
		t.Errorf("Detail = %q, want %q", entry.Detail, "added helper")
	}
	if len(entry.Files) != 1 {
		t.Fatalf("expected 1 file entry, got %d", len(entry.Files))
	}
	if entry.Files[0].Path != srcPath {
		t.Errorf("Files[0].Path = %q, want %q", entry.Files[0].Path, srcPath)
	}
	if entry.Files[0].Snapshot == "" {
		t.Error("expected non-empty snapshot name")
	}

	// Round-trip: LoadJournal should return the same entry.
	loaded, err := LoadJournal()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].Command != "insert" {
		t.Errorf("loaded journal mismatch: %+v", loaded)
	}
}

func TestRecordOperation_TracksCreatedFiles(t *testing.T) {
	dir := t.TempDir()
	withWorkDir(t, dir)

	newFile := filepath.Join(dir, "new.go")
	entry, err := RecordOperation("create", "", nil, []string{newFile})
	if err != nil {
		t.Fatalf("RecordOperation: %v", err)
	}
	if len(entry.Files) != 1 {
		t.Fatalf("expected 1 file entry, got %d", len(entry.Files))
	}
	if !entry.Files[0].Created {
		t.Error("expected Created=true for newly created file")
	}
}

func TestRecordOperation_MultipleEntriesAccumulate(t *testing.T) {
	dir := t.TempDir()
	withWorkDir(t, dir)

	if _, err := RecordOperation("op1", "", nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := RecordOperation("op2", "", nil, nil); err != nil {
		t.Fatal(err)
	}

	entries, err := LoadJournal()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Command != "op1" || entries[1].Command != "op2" {
		t.Errorf("unexpected order: %v %v", entries[0].Command, entries[1].Command)
	}
}

// ---- UndoLast ----

func TestUndoLast_RestoresModifiedFile(t *testing.T) {
	dir := t.TempDir()
	withWorkDir(t, dir)

	srcPath := filepath.Join(dir, "src.go")
	origContent := []byte("package main\n// original\n")
	if err := os.WriteFile(srcPath, origContent, 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := RecordOperation("mutate", "", map[string][]byte{srcPath: origContent}, nil); err != nil {
		t.Fatal(err)
	}

	// Simulate mutation
	if err := os.WriteFile(srcPath, []byte("package main\n// changed\n"), 0644); err != nil {
		t.Fatal(err)
	}

	undone, count, err := UndoLast()
	if err != nil {
		t.Fatalf("UndoLast: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}
	if undone.Command != "mutate" {
		t.Errorf("undone.Command = %q, want %q", undone.Command, "mutate")
	}

	got, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(origContent) {
		t.Errorf("file content after undo = %q, want %q", got, origContent)
	}

	// Journal should be empty after undo.
	entries, err := LoadJournal()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("expected empty journal after undo, got %d entries", len(entries))
	}
}

func TestUndoLast_RemovesCreatedFile(t *testing.T) {
	dir := t.TempDir()
	withWorkDir(t, dir)

	createdFile := filepath.Join(dir, "created.go")
	if err := os.WriteFile(createdFile, []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := RecordOperation("create", "", nil, []string{createdFile}); err != nil {
		t.Fatal(err)
	}

	_, _, err := UndoLast()
	if err != nil {
		t.Fatalf("UndoLast: %v", err)
	}

	if _, err := os.Stat(createdFile); !os.IsNotExist(err) {
		t.Error("expected created file to be deleted by undo")
	}
}

func TestUndoLast_EmptyJournal_ReturnsError(t *testing.T) {
	withWorkDir(t, t.TempDir())

	_, _, err := UndoLast()
	if err == nil {
		t.Fatal("expected error when undoing empty journal")
	}
}

func TestUndoLast_StackOrder(t *testing.T) {
	dir := t.TempDir()
	withWorkDir(t, dir)

	f1 := filepath.Join(dir, "a.go")
	f2 := filepath.Join(dir, "b.go")
	_ = os.WriteFile(f1, []byte("a"), 0644)
	_ = os.WriteFile(f2, []byte("b"), 0644)

	if _, err := RecordOperation("first", "", map[string][]byte{f1: []byte("a")}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := RecordOperation("second", "", map[string][]byte{f2: []byte("b")}, nil); err != nil {
		t.Fatal(err)
	}

	// Undo pops the most recent entry first.
	undone, _, err := UndoLast()
	if err != nil {
		t.Fatal(err)
	}
	if undone.Command != "second" {
		t.Errorf("expected second to be undone first, got %q", undone.Command)
	}

	entries, _ := LoadJournal()
	if len(entries) != 1 || entries[0].Command != "first" {
		t.Errorf("expected only first to remain, got %+v", entries)
	}
}

// ---- DropJournalEntry ----

func TestDropJournalEntry_RemovesTargetEntry(t *testing.T) {
	dir := t.TempDir()
	withWorkDir(t, dir)

	e1, err := RecordOperation("alpha", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RecordOperation("beta", "", nil, nil); err != nil {
		t.Fatal(err)
	}

	if err := DropJournalEntry(e1.ID); err != nil {
		t.Fatalf("DropJournalEntry: %v", err)
	}

	entries, err := LoadJournal()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Command != "beta" {
		t.Errorf("expected only beta remaining, got %+v", entries)
	}
}

func TestDropJournalEntry_UnknownIDIsNoop(t *testing.T) {
	dir := t.TempDir()
	withWorkDir(t, dir)

	if _, err := RecordOperation("only", "", nil, nil); err != nil {
		t.Fatal(err)
	}

	if err := DropJournalEntry("nonexistent-id"); err != nil {
		t.Fatalf("DropJournalEntry with unknown ID: %v", err)
	}

	entries, _ := LoadJournal()
	if len(entries) != 1 {
		t.Errorf("expected entry to remain after noop drop, got %d entries", len(entries))
	}
}

// withWorkDir changes the current working directory to dir for the duration of
// the test. Tests using this must NOT call t.Parallel() — os.Chdir is
// process-global.
func withWorkDir(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
}
