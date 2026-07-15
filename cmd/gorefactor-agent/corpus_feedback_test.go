package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFailureCorpusSection_Empty(t *testing.T) {
	dir := t.TempDir()
	if got := failureCorpusSection(dir); got != "" {
		t.Errorf("expected empty section for cold repo, got %q", got)
	}
}

func writeCorpus(t *testing.T, dir string, entries ...failureEntry) {
	t.Helper()
	for _, e := range entries {
		logFailure(dir, e)
	}
}

func TestFailureCorpusSection_AggregatesTopTools(t *testing.T) {
	dir := t.TempDir()
	writeCorpus(t, dir,
		failureEntry{Kind: failRejectedOp, Tool: "replace_code", Reason: "codePattern must be a complete statement at line 10"},
		failureEntry{Kind: failRejectedOp, Tool: "replace_code", Reason: "codePattern must be a complete statement at line 42"},
		failureEntry{Kind: failRejectedOp, Tool: "replace_code", Reason: "codePattern must be a complete statement at line 88"},
		failureEntry{Kind: failRejectedOp, Tool: "extract_method", Reason: "no complete statements in lines 5-9"},
		failureEntry{Kind: failCapabilityGap, Reason: "no command to move across packages"},
		failureEntry{Kind: failBudgetHit, Reason: "budget 1000 exhausted"},
	)
	got := failureCorpusSection(dir)
	if got == "" {
		t.Fatal("expected non-empty section")
	}
	// replace_code has the most rejections and must appear first, with a count.
	if !strings.Contains(got, "replace_code rejected 3×") {
		t.Errorf("expected replace_code count of 3, got:\n%s", got)
	}
	riReplace := strings.Index(got, "replace_code")
	riExtract := strings.Index(got, "extract_method")
	if riReplace < 0 || riExtract < 0 || riReplace > riExtract {
		t.Errorf("expected replace_code ranked before extract_method:\n%s", got)
	}
	if !strings.Contains(got, "capability-gap") {
		t.Errorf("expected capability-gap line, got:\n%s", got)
	}
	if !strings.Contains(got, "budget exhaustion") {
		t.Errorf("expected budget line, got:\n%s", got)
	}
	// The digit-normalized reason groups all three variants; a raw example shows.
	if !strings.Contains(got, "complete statement") {
		t.Errorf("expected representative reason, got:\n%s", got)
	}
}

func TestFailureCorpusSection_OnlyPuntsNoTools(t *testing.T) {
	dir := t.TempDir()
	// A punt with no tool should not produce tool bullets, and a lone punt
	// (not a capability gap / budget hit) yields an empty section.
	writeCorpus(t, dir, failureEntry{Kind: failPunt, Reason: "handed back"})
	if got := failureCorpusSection(dir); got != "" {
		t.Errorf("expected empty section for punt-only corpus, got:\n%s", got)
	}
}

func TestReadFailureCorpus_TrailingWindow(t *testing.T) {
	dir := t.TempDir()
	// Write more than the scan cap; only the trailing window is parsed.
	for i := 0; i < 10; i++ {
		logFailure(dir, failureEntry{Kind: failRejectedOp, Tool: "t", Reason: "r"})
	}
	entries := readFailureCorpus(dir, 4)
	if len(entries) != 4 {
		t.Errorf("expected trailing window of 4, got %d", len(entries))
	}
}

func TestReadFailureCorpus_SkipsMalformed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, corpusRelPath)
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	content := `{"kind":"rejected_op","tool":"a","reason":"x"}
not json
{"kind":"rejected_op","tool":"b","reason":"y"}
`
	_ = os.WriteFile(path, []byte(content), 0o644)
	entries := readFailureCorpus(dir, 0)
	if len(entries) != 2 {
		t.Errorf("expected 2 valid entries (malformed skipped), got %d", len(entries))
	}
}
