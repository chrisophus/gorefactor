package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/chrisophus/gorefactor/orchestrator"
)

// adherenceRepo builds a temp git repo with one baseline commit, chdirs into
// it (restored on cleanup), and returns the dir. Hermetic: local git only.
func adherenceRepo(t *testing.T, baseline map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range baseline {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-q")
	run("add", "-A")
	run("-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "baseline")

	prev, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(prev) })
	return dir
}

// writeJournal writes a synthetic .gorefactor/journal.json in the cwd.
func writeJournal(t *testing.T, entries []orchestrator.JournalEntry) {
	t.Helper()
	if err := os.MkdirAll(".gorefactor", 0o755); err != nil {
		t.Fatal(err)
	}
	b, _ := json.MarshalIndent(entries, "", "  ")
	if err := os.WriteFile(filepath.Join(".gorefactor", "journal.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestComputeAdherenceClassifiesAndTimeBounds(t *testing.T) {
	adherenceRepo(t, map[string]string{
		"existing_mod.go": "package p\n\nfunc A() {}\n",
		"existing_raw.go": "package p\n\nfunc B() {}\n",
	})

	recent := time.Now().Add(time.Hour)  // safely after the baseline commit
	stale := time.Now().Add(-time.Hour)  // before the baseline ⇒ must not count
	writeJournal(t, []orchestrator.JournalEntry{
		{Command: "replace-body", Timestamp: recent, Files: []orchestrator.JournalFile{{Path: "existing_mod.go"}}},
		{Command: "create", Timestamp: recent, Files: []orchestrator.JournalFile{{Path: "created_grf.go", Created: true}}},
		// Stale entry for existing_raw.go — older than the baseline, so the
		// time bound must exclude it and the file stays "raw".
		{Command: "replace-body", Timestamp: stale, Files: []orchestrator.JournalFile{{Path: "existing_raw.go"}}},
	})

	// Working-tree changes after the baseline.
	mustWrite(t, "existing_mod.go", "package p\n\nfunc A() int { return 1 }\n")
	mustWrite(t, "existing_raw.go", "package p\n\nfunc B() int { return 2 }\n")
	mustWrite(t, "created_grf.go", "package p\n\nfunc C() {}\n")
	mustWrite(t, "created_raw.go", "package p\n\nfunc D() {}\n")

	rep, err := computeAdherence("HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if rep.ModifiedTotal != 2 || rep.ModifiedAttributed != 1 {
		t.Errorf("modified: got %d/%d, want 1/2", rep.ModifiedAttributed, rep.ModifiedTotal)
	}
	if len(rep.ModifiedRaw) != 1 || rep.ModifiedRaw[0] != "existing_raw.go" {
		t.Errorf("modifiedRaw = %v, want [existing_raw.go] (stale journal entry must not attribute)", rep.ModifiedRaw)
	}
	if rep.CreatedTotal != 2 || rep.CreatedAttributed != 1 {
		t.Errorf("created: got %d/%d, want 1/2", rep.CreatedAttributed, rep.CreatedTotal)
	}
	if r, ok := rep.ratio(); !ok || r != 0.5 {
		t.Errorf("ratio = %v (ok=%v), want 0.5", r, ok)
	}
}

func TestComputeAdherenceNoModificationsRatioNA(t *testing.T) {
	adherenceRepo(t, map[string]string{"a.go": "package p\n"})
	// Only a new file — a create-only diff. Ratio must be undefined, not 0%.
	mustWrite(t, "b.go", "package p\n\nfunc B() {}\n")
	rep, err := computeAdherence("HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := rep.ratio(); ok {
		t.Error("create-only diff should have an undefined ratio, not a computed one")
	}
	if rep.CreatedTotal != 1 {
		t.Errorf("createdTotal = %d, want 1", rep.CreatedTotal)
	}
}

func TestAdherenceRelevantExclusions(t *testing.T) {
	cases := map[string]bool{
		"foo.go":               true,
		"foo_test.go":          true, // tests count — still code we prefer to edit via gorefactor
		"vendor/x/y.go":        false,
		".gorefactor/notes.go": false,
		"pkg/testdata/z.go":    false,
		"README.md":            false,
	}
	for path, want := range cases {
		if got := adherenceRelevant(path); got != want {
			t.Errorf("adherenceRelevant(%q) = %v, want %v", path, got, want)
		}
	}
}

func mustWrite(t *testing.T, name, content string) {
	t.Helper()
	if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
