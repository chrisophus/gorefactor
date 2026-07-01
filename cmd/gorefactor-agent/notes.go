package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Phase 4: persistent cross-session notes.
//
// Every session currently re-learns repo facts a previous session
// already established (flaky packages, targeting strategies that fail
// here, prior punt reasons). Because input tokens dominate agentic cost
// and notes are re-read at every session start, a fixed note-writing
// cost is paid once and amortised over every future session that reads
// it. Notes live in .gorefactor/notes.md; they are loaded into the
// system prompt at agent start and appended only via a dedicated tool
// call (never a free-form file write), preserving the harness principle
// that the model does not directly edit files.

const notesRelPath = ".gorefactor/notes.md"

// notesCompactionThreshold is the line count past which notes.md should
// be run through the crucible purify pass (English -> AISP -> English)
// to compact accumulated prose into minimal unambiguous statements and
// surface contradictions between sessions. Initial default; tunable
// after the first firing. The purify pass itself is an out-of-band
// frontier-token cost, so appendNote only emits an advisory when the
// threshold is crossed -- it does not shell out to crucible.
const notesCompactionThreshold = 200

// noteCategories are the recognised note buckets. An unknown category is
// accepted verbatim so the schema stays loose enough to be cheap to
// write, but the known set keeps notes groupable for compaction.
var noteCategories = map[string]bool{
	"repo_fact":       true,
	"failed_strategy": true,
	"flaky_test":      true,
	"open_punt":       true,
}

// loadNotes returns the notes.md body for dir, or "" if there are none.
func loadNotes(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, notesRelPath))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// notesPromptSection renders the loaded notes as a system-prompt block,
// or "" when there are none so a cold repo pays nothing.
func notesPromptSection(dir string) string {
	body := loadNotes(dir)
	if body == "" {
		return ""
	}
	return "\n\nPERSISTENT NOTES (facts prior sessions established about this repo -- " +
		"trust them before re-discovering):\n" + body + "\n"
}

// appendNote adds one categorised note to notes.md under dir and returns
// a compact confirmation. When the file crosses notesCompactionThreshold
// lines it appends a one-line advisory that a crucible purify pass is due.
func appendNote(dir, category, text string) string {
	category = strings.TrimSpace(category)
	text = strings.TrimSpace(text)
	if text == "" {
		return "ERROR: note text is required"
	}
	if category == "" {
		category = "repo_fact"
	}
	path := filepath.Join(dir, notesRelPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "ERROR: could not create .gorefactor: " + err.Error()
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		header := "# gorefactor persistent notes\n\n" +
			"Facts, failed strategies, flaky tests, and open punts carried across sessions.\n"
		_ = os.WriteFile(path, []byte(header), 0o644)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return "ERROR: could not open notes: " + err.Error()
	}
	line := fmt.Sprintf("- [%s] %s _(%s)_\n",
		category, text, time.Now().UTC().Format("2006-01-02"))
	if _, err := f.WriteString(line); err != nil {
		_ = f.Close()
		return "ERROR: could not write note: " + err.Error()
	}
	_ = f.Close()

	msg := fmt.Sprintf("noted [%s]: %s", category, trim(text, 120))
	if n := countLines(path); n > notesCompactionThreshold {
		msg += fmt.Sprintf(" (notes.md is %d lines, over the %d-line threshold; "+
			"run a crucible purify pass to compact and surface contradictions)",
			n, notesCompactionThreshold)
	}
	return msg
}

func countLines(path string) int {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	return strings.Count(string(b), "\n")
}
