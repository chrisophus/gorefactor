package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
}
