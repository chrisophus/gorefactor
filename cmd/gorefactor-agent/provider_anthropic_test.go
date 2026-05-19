package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestAnthropicChatToolsTranslation verifies the OpenAI<->Anthropic
// translation in both directions against a fake Messages endpoint:
// system hoisted out, OpenAI tool calls -> tool_use, consecutive tool
// results merged into one user turn, tools -> input_schema, and the
// response's text+tool_use blocks mapped back to the OpenAI shape.
func TestAnthropicChatToolsTranslation(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if r.Header.Get("anthropic-version") == "" {
			t.Error("missing anthropic-version header")
		}
		gotBody, _ = io.ReadAll(r.Body)
		io.WriteString(w, `{
			"content":[
				{"type":"text","text":"hello "},
				{"type":"tool_use","id":"tu_9","name":"rename","input":{"from":"a","to":"b"}}
			],
			"stop_reason":"tool_use",
			"usage":{"input_tokens":11,"output_tokens":7}
		}`)
	}))
	defer srv.Close()

	p := newAnthropicProvider(srv.URL, "k", "test-model")

	messages := []chatMessage{
		{Role: "system", Content: "SYS"},
		{Role: "user", Content: "do X"},
		{Role: "assistant", Content: "thinking", ToolCalls: []toolCall{{
			ID: "call_1", Type: "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{Name: "find", Arguments: `{"q":"foo"}`},
		}}},
		{Role: "tool", ToolCallID: "call_1", Content: "result A"},
		{Role: "tool", ToolCallID: "call_1b", Content: "result B"},
	}
	tools := []toolDef{{Type: "function", Function: toolDefFunction{
		Name: "find", Description: "finds things",
		Parameters: map[string]any{"type": "object"},
	}}}

	out, err := p.ChatTools(context.Background(), messages, tools)
	if err != nil {
		t.Fatalf("ChatTools error: %v", err)
	}

	// --- response mapping ---
	if out.Role != "assistant" {
		t.Errorf("role = %q, want assistant", out.Role)
	}
	if out.Content != "hello " {
		t.Errorf("content = %q, want %q", out.Content, "hello ")
	}
	if len(out.ToolCalls) != 1 {
		t.Fatalf("toolcalls = %d, want 1", len(out.ToolCalls))
	}
	tc := out.ToolCalls[0]
	if tc.ID != "tu_9" || tc.Type != "function" || tc.Function.Name != "rename" {
		t.Errorf("toolcall = %+v", tc)
	}
	var args map[string]string
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		t.Fatalf("tool args not valid JSON: %v (%q)", err, tc.Function.Arguments)
	}
	if args["from"] != "a" || args["to"] != "b" {
		t.Errorf("tool args = %v", args)
	}
	if pt, ct := p.Tokens(); pt != 11 || ct != 7 {
		t.Errorf("tokens = (%d,%d), want (11,7)", pt, ct)
	}

	// --- request translation ---
	var got struct {
		System      string  `json:"system"`
		Model       string  `json:"model"`
		MaxTokens   int     `json:"max_tokens"`
		Temperature float64 `json:"temperature"`
		Tools       []struct {
			Name        string         `json:"name"`
			Description string         `json:"description"`
			InputSchema map[string]any `json:"input_schema"`
		} `json:"tools"`
		Messages []struct {
			Role    string           `json:"role"`
			Content []map[string]any `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(gotBody, &got); err != nil {
		t.Fatalf("request body not JSON: %v", err)
	}
	if got.System != "SYS" {
		t.Errorf("system = %q, want SYS", got.System)
	}
	if got.Model != "test-model" || got.MaxTokens == 0 || got.Temperature != 0 {
		t.Errorf("model/maxtokens/temp = %q/%d/%v", got.Model, got.MaxTokens, got.Temperature)
	}
	if len(got.Tools) != 1 || got.Tools[0].Name != "find" ||
		got.Tools[0].Description != "finds things" || got.Tools[0].InputSchema["type"] != "object" {
		t.Errorf("tools translated wrong: %+v", got.Tools)
	}
	if len(got.Messages) != 3 {
		t.Fatalf("messages = %d, want 3 (system hoisted, tools merged): %+v", len(got.Messages), got.Messages)
	}
	if got.Messages[0].Role != "user" || got.Messages[0].Content[0]["text"] != "do X" {
		t.Errorf("msg0 = %+v", got.Messages[0])
	}
	asst := got.Messages[1]
	if asst.Role != "assistant" || len(asst.Content) != 2 {
		t.Fatalf("msg1 = %+v", asst)
	}
	if asst.Content[0]["type"] != "text" || asst.Content[0]["text"] != "thinking" {
		t.Errorf("msg1 text block = %+v", asst.Content[0])
	}
	tu := asst.Content[1]
	if tu["type"] != "tool_use" || tu["id"] != "call_1" || tu["name"] != "find" {
		t.Errorf("msg1 tool_use = %+v", tu)
	}
	if input, ok := tu["input"].(map[string]any); !ok || input["q"] != "foo" {
		t.Errorf("msg1 tool_use input = %v", tu["input"])
	}
	merged := got.Messages[2]
	if merged.Role != "user" || len(merged.Content) != 2 {
		t.Fatalf("consecutive tool results not merged into one user turn: %+v", merged)
	}
	for i, want := range []struct{ id, content string }{
		{"call_1", "result A"}, {"call_1b", "result B"},
	} {
		b := merged.Content[i]
		if b["type"] != "tool_result" || b["tool_use_id"] != want.id || b["content"] != want.content {
			t.Errorf("tool_result[%d] = %+v, want %v", i, b, want)
		}
	}
}

// TestAnthropicChatToolsHTTPError verifies a non-200 surfaces as an error
// (the sensor the diagnostics also log) rather than a silent empty turn.
func TestAnthropicChatToolsHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		io.WriteString(w, `{"error":{"message":"rate limited"}}`)
	}))
	defer srv.Close()

	p := newAnthropicProvider(srv.URL, "k", "test-model")
	_, err := p.ChatTools(context.Background(), []chatMessage{{Role: "user", Content: "hi"}}, nil)
	if err == nil {
		t.Fatal("expected error on HTTP 429, got nil")
	}
}
