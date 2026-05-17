package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Provider is the minimal LLM surface the driver needs: turn a
// (system, user) pair into a single completion string. Keeping the
// interface this small is deliberate harness engineering -- the model
// only ever fills a constrained schema, so we never need streaming,
// tools, or multi-turn state here.
type Provider interface {
	Complete(ctx context.Context, system, user string) (string, error)
}

// mockProvider returns scripted responses. It exists so the whole
// driver loop can be tested deterministically and offline -- the
// "mock" sensor in harness terms.
type mockProvider struct {
	responses []string
	calls     int
}

func (m *mockProvider) Complete(_ context.Context, _, _ string) (string, error) {
	if m.calls >= len(m.responses) {
		return "", fmt.Errorf("mockProvider: no scripted response for call %d", m.calls+1)
	}
	r := m.responses[m.calls]
	m.calls++
	return r, nil
}

// openAIProvider talks to any OpenAI-compatible /chat/completions
// endpoint. That single shape covers cloud OpenAI, Anthropic via an
// OpenAI-compat gateway, and local servers (llama.cpp, Ollama, vLLM) --
// so "use a cheap or local model" is just a base-URL/model swap.
type openAIProvider struct {
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
}

func newOpenAIProvider(baseURL, apiKey, model string) *openAIProvider {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	return &openAIProvider{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		model:   model,
		client:  &http.Client{Timeout: 120 * time.Second},
	}
}

// schemaCompleter is implemented by providers that can enforce a JSON
// schema at decode time. The loop type-asserts for it and falls back
// to plain Complete (mock, Anthropic) when absent.
type schemaCompleter interface {
	CompleteSchema(ctx context.Context, system, user, schema string) (string, error)
}

func (p *openAIProvider) Complete(ctx context.Context, system, user string) (string, error) {
	return p.complete(ctx, system, user, "")
}

// CompleteSchema sends response_format=json_schema. Ollama (>=0.5) and
// OpenAI structured outputs both honor this on the /chat/completions
// endpoint; the model is grammar-constrained to the schema.
func (p *openAIProvider) CompleteSchema(ctx context.Context, system, user, schema string) (string, error) {
	return p.complete(ctx, system, user, schema)
}

func (p *openAIProvider) complete(ctx context.Context, system, user, schema string) (string, error) {
	reqBody := map[string]any{
		"model": p.model,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
		"temperature": 0,
	}
	if schema != "" {
		reqBody["response_format"] = map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   "refactoring_plan",
				"schema": json.RawMessage(schema),
			},
		}
	}
	buf, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.baseURL+"/chat/completions", bytes.NewReader(buf))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("provider HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("decode provider response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("provider returned no choices")
	}
	return parsed.Choices[0].Message.Content, nil
}

// --- Tool-calling surface (Arm D) -----------------------------------
//
// The agentic loop needs multi-turn function calling, not single-shot
// completion. These types mirror the OpenAI /chat/completions tool
// protocol, which Ollama (qwen2.5-coder) honors.

type chatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []toolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type toolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type toolDef struct {
	Type     string          `json:"type"`
	Function toolDefFunction `json:"function"`
}

type toolDefFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// toolChatter is the agentic provider surface: one round trip given the
// conversation so far + the tool catalog, returning the assistant turn
// (content and/or tool calls). The loop owns iteration & tool exec.
type toolChatter interface {
	ChatTools(ctx context.Context, messages []chatMessage, tools []toolDef) (chatMessage, error)
}

// ChatTools implements toolChatter for any OpenAI-compatible endpoint.
func (p *openAIProvider) ChatTools(ctx context.Context, messages []chatMessage, tools []toolDef) (chatMessage, error) {
	reqBody := map[string]any{
		"model":       p.model,
		"messages":    messages,
		"temperature": 0,
	}
	if len(tools) > 0 {
		reqBody["tools"] = tools
		reqBody["tool_choice"] = "auto"
	}
	buf, err := json.Marshal(reqBody)
	if err != nil {
		return chatMessage{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.baseURL+"/chat/completions", bytes.NewReader(buf))
	if err != nil {
		return chatMessage{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return chatMessage{}, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return chatMessage{}, fmt.Errorf("provider HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var parsed struct {
		Choices []struct {
			Message chatMessage `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return chatMessage{}, fmt.Errorf("decode tool response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return chatMessage{}, fmt.Errorf("provider returned no choices")
	}
	msg := parsed.Choices[0].Message
	// qwen via Ollama emits tool calls as JSON in `content` (and/or
	// Hermes <tool_call> tags) rather than the OpenAI `tool_calls`
	// field. The model IS tool-calling; absorb its dialect (harness
	// absorbs model variety) instead of mistaking it for prose.
	if len(msg.ToolCalls) == 0 {
		if tcs := parseContentToolCalls(msg.Content); len(tcs) > 0 {
			msg.ToolCalls = tcs
			msg.Content = ""
		}
	}
	return msg, nil
}

// parseContentToolCalls recovers tool calls a model put in message
// content. Handles Hermes <tool_call>{...}</tool_call> blocks and bare
// {"name","arguments"} objects, with or without ``` fences.
func parseContentToolCalls(content string) []toolCall {
	s := strings.TrimSpace(content)
	if s == "" {
		return nil
	}
	var blobs []string
	if strings.Contains(s, "<tool_call>") {
		for {
			i := strings.Index(s, "<tool_call>")
			if i < 0 {
				break
			}
			rest := s[i+len("<tool_call>"):]
			j := strings.Index(rest, "</tool_call>")
			if j < 0 {
				blobs = append(blobs, rest)
				break
			}
			blobs = append(blobs, rest[:j])
			s = rest[j+len("</tool_call>"):]
		}
	} else {
		t := strings.TrimPrefix(s, "```json")
		t = strings.TrimPrefix(t, "```")
		t = strings.TrimSuffix(t, "```")
		if obj := firstJSONObject(t); obj != "" {
			blobs = append(blobs, obj)
		}
	}

	var calls []toolCall
	for n, b := range blobs {
		obj := firstJSONObject(b)
		if obj == "" {
			continue
		}
		var raw struct {
			Name       string          `json:"name"`
			Arguments  json.RawMessage `json:"arguments"`
			Parameters json.RawMessage `json:"parameters"`
		}
		if json.Unmarshal([]byte(obj), &raw) != nil || raw.Name == "" {
			continue
		}
		args := raw.Arguments
		if len(args) == 0 {
			args = raw.Parameters
		}
		argStr := strings.TrimSpace(string(args))
		if len(argStr) > 1 && argStr[0] == '"' {
			var unq string
			if json.Unmarshal([]byte(argStr), &unq) == nil {
				argStr = unq
			}
		}
		if argStr == "" {
			argStr = "{}"
		}
		var c toolCall
		c.ID = fmt.Sprintf("call_%d", n)
		c.Type = "function"
		c.Function.Name = raw.Name
		c.Function.Arguments = argStr
		calls = append(calls, c)
	}
	return calls
}

// firstJSONObject returns the first balanced {...} in s (string-aware).
func firstJSONObject(s string) string {
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return ""
	}
	depth, inStr, esc := 0, false, false
	for i := start; i < len(s); i++ {
		c := s[i]
		switch {
		case esc:
			esc = false
		case c == '\\' && inStr:
			esc = true
		case c == '"':
			inStr = !inStr
		case inStr:
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}

// providerFromFlags builds the real provider from CLI/env config.
// kind selects the wire protocol: "anthropic" for the native Messages
// API (cheap Claude models), anything else for OpenAI-compatible.
func providerFromFlags(kind, baseURL, model string) Provider {
	switch strings.ToLower(kind) {
	case "anthropic":
		key := os.Getenv("ANTHROPIC_API_KEY")
		return newAnthropicProvider(baseURL, key, model)
	default:
		key := os.Getenv("OPENAI_API_KEY")
		if key == "" {
			key = os.Getenv("LLM_API_KEY")
		}
		return newOpenAIProvider(baseURL, key, model)
	}
}
