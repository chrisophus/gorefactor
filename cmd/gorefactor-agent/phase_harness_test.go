package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- Phase 1: tool-output masking ------------------------------------

func TestMaskStaleToolOutputs(t *testing.T) {
	// Build system + task + several (assistant,tool) rounds.
	msgs := []chatMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "TASK: do it"},
	}
	for i := 0; i < 6; i++ {
		id := string(rune('a' + i))
		asst := chatMessage{Role: "assistant"}
		asst.ToolCalls = []toolCall{{ID: id}}
		asst.ToolCalls[0].Function.Name = "lint_path"
		msgs = append(msgs, asst,
			chatMessage{Role: "tool", ToolCallID: id,
				Content: "finding one\nfinding two\nfinding three"})
	}

	out := maskStaleToolOutputs(msgs, 2)

	// Structure preserved: same count, system/task untouched.
	if len(out) != len(msgs) {
		t.Fatalf("message count changed: %d -> %d", len(msgs), len(out))
	}
	if out[0].Content != "sys" || out[1].Content != "TASK: do it" {
		t.Fatalf("system/task must never be masked: %+v", out[:2])
	}
	// The last tool message (most recent round) keeps its full body.
	last := out[len(out)-1]
	if strings.HasPrefix(last.Content, maskMarker) {
		t.Fatalf("most-recent tool result must not be masked: %q", last.Content)
	}
	// An early tool message is stubbed and names the tool + size.
	early := out[3] // first tool message
	if !strings.HasPrefix(early.Content, maskMarker) {
		t.Fatalf("stale tool result should be masked, got %q", early.Content)
	}
	if !strings.Contains(early.Content, "lint_path") {
		t.Fatalf("stub should name the tool: %q", early.Content)
	}

	// Idempotent: masking again changes nothing.
	if again := maskStaleToolOutputs(out, 2); again[3].Content != out[3].Content {
		t.Fatalf("masking not idempotent: %q vs %q", again[3].Content, out[3].Content)
	}

	// Too few rounds -> nothing masked.
	short := msgs[:4] // sys, task, one asst, one tool
	if got := maskStaleToolOutputs(short, 3); got[3].Content != short[3].Content {
		t.Fatalf("short history should be untouched")
	}
}

// --- Phase 4: persistent notes ---------------------------------------

func TestNotesRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if loadNotes(dir) != "" {
		t.Fatalf("cold repo should have no notes")
	}
	if notesPromptSection(dir) != "" {
		t.Fatalf("cold repo prompt section should be empty")
	}

	msg := appendNote(dir, "flaky_test", "package foo is flaky under -race")
	if strings.HasPrefix(msg, "ERROR") {
		t.Fatalf("appendNote failed: %s", msg)
	}
	body := loadNotes(dir)
	if !strings.Contains(body, "flaky_test") || !strings.Contains(body, "package foo is flaky") {
		t.Fatalf("note not persisted: %q", body)
	}
	sec := notesPromptSection(dir)
	if !strings.Contains(sec, "PERSISTENT NOTES") || !strings.Contains(sec, "flaky") {
		t.Fatalf("prompt section missing notes: %q", sec)
	}

	// Empty text is rejected; empty category defaults to repo_fact.
	if !strings.HasPrefix(appendNote(dir, "repo_fact", ""), "ERROR") {
		t.Fatalf("empty note text should be rejected")
	}
	if m := appendNote(dir, "", "some fact"); strings.HasPrefix(m, "ERROR") {
		t.Fatalf("default category should be accepted: %s", m)
	}
	if !strings.Contains(loadNotes(dir), "[repo_fact]") {
		t.Fatalf("empty category should default to repo_fact")
	}
}

func TestNotesCompactionAdvisory(t *testing.T) {
	dir := t.TempDir()
	var last string
	for i := 0; i < notesCompactionThreshold+5; i++ {
		last = appendNote(dir, "repo_fact", "fact number for volume")
	}
	if !strings.Contains(last, "crucible purify") {
		t.Fatalf("expected compaction advisory past %d lines, got %q",
			notesCompactionThreshold, last)
	}
}

// --- Phase 6: failure corpus -----------------------------------------

func TestFailureCorpus(t *testing.T) {
	dir := t.TempDir()

	// A mutation-tool rejection is recorded.
	recordRejectedOp(dir, "rename_declaration",
		`{"function":"Foo","new_name":"Bar"}`,
		"FAILED: no declaration Foo", "rename Foo to Bar")
	// A read-only tool "not found" is NOT recorded.
	recordRejectedOp(dir, "find_references", `{"symbol":"X"}`,
		"ERROR: not found", "where is X")
	// A successful mutation is NOT recorded.
	recordRejectedOp(dir, "rename_declaration",
		`{"function":"A","new_name":"B"}`, "applied rename_declaration on a.go", "")

	entries := readCorpus(t, dir)
	if len(entries) != 1 {
		t.Fatalf("expected exactly 1 corpus entry, got %d: %+v", len(entries), entries)
	}
	e := entries[0]
	if e.Kind != failRejectedOp || e.Tool != "rename_declaration" {
		t.Fatalf("bad entry: %+v", e)
	}
	if e.TS == "" || !strings.Contains(e.Reason, "no declaration Foo") {
		t.Fatalf("entry missing ts/reason: %+v", e)
	}
}

func TestIsOpRejection(t *testing.T) {
	cases := map[string]bool{
		"ERROR: boom":                 true,
		"FAILED: no such symbol":      true,
		"applied rename on x.go":      false,
		"moved Foo from a.go to b.go": false,
		"  ERROR leading space":       true,
	}
	for in, want := range cases {
		if got := isOpRejection(in); got != want {
			t.Errorf("isOpRejection(%q)=%v want %v", in, got, want)
		}
	}
}

// --- Phase 3: blast-radius instrumentation ---------------------------

func TestPrimarySymbol(t *testing.T) {
	cases := map[string]string{
		"extract the ValidateOrder logic":   "ValidateOrder",
		"rename Parser:Parse to Parse2":     "Parser:Parse",
		"use a range loop":                  "",
		"move PaymentService to a new file": "PaymentService",
	}
	for spec, want := range cases {
		if got := primarySymbol(spec); got != want {
			t.Errorf("primarySymbol(%q)=%q want %q", spec, got, want)
		}
	}
}

func TestEmitRunMetricsBlastRadius(t *testing.T) {
	var buf bytes.Buffer
	emitRunMetrics(&buf, nil, nil, 3, 42)
	out := buf.String()
	if !strings.Contains(out, `"blast_radius":42`) {
		t.Fatalf("RUN_METRICS missing blast_radius: %s", out)
	}
	if !strings.Contains(out, `"outcome":"fixed"`) {
		t.Fatalf("RUN_METRICS missing outcome: %s", out)
	}
}

// --- Phase 2: token budget -------------------------------------------

// tokenMock reports a fixed, large token count so the budget check trips
// on the first step regardless of what it scripts.
type tokenMock struct {
	mockToolProvider
	toks int
}

func (m *tokenMock) Tokens() (int, int) { return m.toks, 0 }

func TestTokenBudgetPunts(t *testing.T) {
	dir := newSampleRepo(t)
	before, _ := os.ReadFile(filepath.Join(dir, "sample.go"))

	mock := &tokenMock{toks: 10_000}
	mock.script = []chatMessage{asstCall("list_symbols", `{"file":"sample.go"}`)}
	mock.repeatLast = true

	var log bytes.Buffer
	err := RunAgenticDriver(context.Background(), mock,
		Config{Spec: "do something", Dir: dir, MaxIter: 8, Budget: 5_000, Out: &log})
	if err == nil || !strings.Contains(err.Error(), "PUNT") {
		t.Fatalf("expected budget punt, got %v\nlog:\n%s", err, log.String())
	}
	rep := extractPuntReport(t, log.String())
	if rep.Kind != "autopunt:budget_exhausted" {
		t.Fatalf("expected autopunt:budget_exhausted, got %q", rep.Kind)
	}
	if !rep.RepoClean {
		t.Fatalf("budget punt must leave a clean tree")
	}
	after, _ := os.ReadFile(filepath.Join(dir, "sample.go"))
	if string(before) != string(after) {
		t.Fatalf("budget punt should not modify files")
	}
	// The budget hit is recorded in the corpus.
	found := false
	for _, e := range readCorpus(t, dir) {
		if e.Kind == failBudgetHit {
			found = true
		}
	}
	if !found {
		t.Fatalf("budget hit not recorded in the failure corpus")
	}
}

func TestTokenBudgetUnlimitedByDefault(t *testing.T) {
	dir := newSampleRepo(t)
	mock := &tokenMock{toks: 1_000_000}
	mock.script = []chatMessage{
		asstCall("replace_code", `{"file":"sample.go","function":"Sum",`+
			`"code_pattern":"for i := 0; i < len(nums); i++ { total = total + nums[i] }",`+
			`"replacement_code":"for _, n := range nums { total += n }"}`),
		asstCall("finish", `{}`),
	}
	var log bytes.Buffer
	// Budget 0 => unlimited: huge token count must not trip a punt.
	if err := RunAgenticDriver(context.Background(), mock,
		Config{Spec: "range loop", Dir: dir, MaxIter: 8, Budget: 0, Out: &log}); err != nil {
		t.Fatalf("budget 0 should be unlimited, got %v\nlog:\n%s", err, log.String())
	}
}

// readCorpus parses every line of the failure corpus under dir.
func readCorpus(t *testing.T, dir string) []failureEntry {
	t.Helper()
	f, err := os.Open(filepath.Join(dir, corpusRelPath))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	defer f.Close()
	var out []failureEntry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e failureEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("corpus line not JSON: %v (%q)", err, line)
		}
		out = append(out, e)
	}
	return out
}
