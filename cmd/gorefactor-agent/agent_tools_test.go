package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// extractPuntReport pulls and parses the JSON the loop emits between
// the <<<PUNT_REPORT ... PUNT_REPORT>>> markers in the run log.
func extractPuntReport(t *testing.T, log string) puntReport {
	t.Helper()
	a := strings.Index(log, "<<<PUNT_REPORT")
	b := strings.Index(log, "PUNT_REPORT>>>")
	if a < 0 || b < 0 || b < a {
		t.Fatalf("no PUNT_REPORT block in log:\n%s", log)
	}
	body := strings.TrimSpace(log[a+len("<<<PUNT_REPORT") : b])
	var rep puntReport
	if err := json.Unmarshal([]byte(body), &rep); err != nil {
		t.Fatalf("punt report not valid JSON: %v\n%s", err, body)
	}
	return rep
}

// mockToolProvider scripts assistant turns for the agentic loop. If
// repeatLast is set, the final scripted turn repeats forever (used to
// drive the budget-exhaustion / autopunt path deterministically).
type mockToolProvider struct {
	script     []chatMessage
	repeatLast bool
	calls      int
}

func (m *mockToolProvider) ChatTools(_ context.Context, _ []chatMessage, _ []toolDef) (chatMessage, error) {
	i := m.calls
	m.calls++
	if i < len(m.script) {
		return m.script[i], nil
	}
	if m.repeatLast && len(m.script) > 0 {
		return m.script[len(m.script)-1], nil
	}
	return chatMessage{Role: "assistant", Content: "(no more script)"}, nil
}

func asstCall(name, argsJSON string) chatMessage {
	var c toolCall
	c.ID = "call_" + name
	c.Type = "function"
	c.Function.Name = name
	c.Function.Arguments = argsJSON
	return chatMessage{Role: "assistant", ToolCalls: []toolCall{c}}
}

func TestCompactMessages(t *testing.T) {
	msgs := []chatMessage{{Role: "system"}, {Role: "user", Content: "task"}}
	for i := 0; i < 20; i++ { // 20 (assistant,tool) pairs
		msgs = append(msgs,
			chatMessage{Role: "assistant", ToolCalls: []toolCall{{ID: "x"}}},
			chatMessage{Role: "tool", ToolCallID: "x"})
	}
	got := compactMessages(msgs, 12)
	if len(got) >= len(msgs) {
		t.Fatalf("not compacted: %d -> %d", len(msgs), len(got))
	}
	if got[0].Role != "system" || got[1].Content != "task" {
		t.Fatalf("system+task not preserved: %+v", got[:2])
	}
	if !strings.Contains(got[2].Content, "elided") {
		t.Fatalf("missing elision marker: %+v", got[2])
	}
	if got[3].Role != "assistant" {
		t.Fatalf("recent window must start on an assistant turn, got %q", got[3].Role)
	}
	// small histories pass through untouched
	small := msgs[:6]
	if len(compactMessages(small, 12)) != len(small) {
		t.Fatalf("small history should be untouched")
	}
}

func TestAgentic_AppliesAndGates(t *testing.T) {
	dir := newSampleRepo(t)

	mock := &mockToolProvider{script: []chatMessage{
		asstCall("replace_code", `{"file":"sample.go","function":"Sum",`+
			`"code_pattern":"for i := 0; i < len(nums); i++ { total = total + nums[i] }",`+
			`"replacement_code":"for _, n := range nums { total += n }"}`),
		asstCall("finish", `{}`),
	}}

	var log bytes.Buffer
	err := RunAgenticDriver(context.Background(), mock,
		Config{Spec: "use a range loop in Sum", Dir: dir, MaxIter: 8, Out: &log})
	if err != nil {
		t.Fatalf("RunAgenticDriver: %v\nlog:\n%s", err, log.String())
	}
	got, _ := os.ReadFile(filepath.Join(dir, "sample.go"))
	if !strings.Contains(string(got), "range nums") {
		t.Fatalf("mutation not applied:\n%s", got)
	}
}

func TestAgentic_PuntRollsBackClean(t *testing.T) {
	dir := newSampleRepo(t)
	before, _ := os.ReadFile(filepath.Join(dir, "sample.go"))

	mock := &mockToolProvider{script: []chatMessage{
		asstCall("punt", `{"reason":"needs an algorithmic change these tools cannot express"}`),
	}}

	var log bytes.Buffer
	err := RunAgenticDriver(context.Background(), mock,
		Config{Spec: "rewrite Sum with a different algorithm", Dir: dir, MaxIter: 8, Out: &log})
	if err == nil || !strings.Contains(err.Error(), "PUNT") {
		t.Fatalf("expected PUNT error, got %v\nlog:\n%s", err, log.String())
	}
	after, _ := os.ReadFile(filepath.Join(dir, "sample.go"))
	if string(before) != string(after) {
		t.Fatalf("punt did not leave the tree clean")
	}

	// The structured warm hand-off must be present and well-formed.
	var pe *puntError
	if !errors.As(err, &pe) {
		t.Fatalf("error is not *puntError: %T", err)
	}
	rep := extractPuntReport(t, log.String())
	if rep.Status != "punt" || rep.Kind != "explicit" {
		t.Fatalf("bad report status/kind: %+v", rep)
	}
	if !rep.RepoClean {
		t.Fatalf("report must assert repo_clean after rollback: %+v", rep)
	}
	if rep.Task == "" || !strings.Contains(rep.Reason, "algorithmic") || len(rep.Trace) == 0 {
		t.Fatalf("report missing warm context: %+v", rep)
	}
	if pe.Report().Reason != rep.Reason {
		t.Fatalf("puntError.Report() out of sync with emitted JSON")
	}
}

func TestAgentic_BudgetExhaustionAutopunts(t *testing.T) {
	dir := newSampleRepo(t)
	before, _ := os.ReadFile(filepath.Join(dir, "sample.go"))

	// Never calls finish — just senses forever. Loop must autopunt.
	mock := &mockToolProvider{
		script:     []chatMessage{asstCall("list_symbols", `{"file":"sample.go"}`)},
		repeatLast: true,
	}

	var log bytes.Buffer
	err := RunAgenticDriver(context.Background(), mock,
		Config{Spec: "do nothing useful", Dir: dir, MaxIter: 3, Out: &log})
	if err == nil || !strings.Contains(err.Error(), "PUNT") {
		t.Fatalf("expected autopunt PUNT, got %v\nlog:\n%s", err, log.String())
	}
	after, _ := os.ReadFile(filepath.Join(dir, "sample.go"))
	if string(before) != string(after) {
		t.Fatalf("autopunt did not leave the tree clean")
	}
	rep := extractPuntReport(t, log.String())
	if rep.Kind != "autopunt:budget" || !rep.RepoClean {
		t.Fatalf("expected clean autopunt:budget report, got %+v", rep)
	}
}
