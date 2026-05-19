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

	// cumulative local-model token usage (free, but the proxy for
	// frontier tokens the junior avoided spending).
	promptToks, completionToks int
}

// tokenStater exposes cumulative model token usage for metrics.
type tokenStater interface {
	Tokens() (prompt, completion int)
}

// usageEnvelope is the OpenAI/Ollama `usage` block (best-effort).
type usageEnvelope struct {
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
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
	endpoint := p.baseURL + "/chat/completions"
	provDebugf("openai ChatTools -> POST %s model=%s msgs=%d tools=%d reqBytes=%d",
		endpoint, p.model, len(messages), len(tools), len(buf))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		endpoint, bytes.NewReader(buf))
	if err != nil {
		return chatMessage{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	start := time.Now()
	resp, err := p.client.Do(req)
	if err != nil {
		provDebugf("openai ChatTools FAILED after %s: %v", time.Since(start), err)
		return chatMessage{}, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	elapsed := time.Since(start)
	if resp.StatusCode != http.StatusOK {
		provDebugf("openai ChatTools <- HTTP %d in %s (%d bytes): %s",
			resp.StatusCode, elapsed, len(body), strings.TrimSpace(string(body)))
		return chatMessage{}, fmt.Errorf("provider HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	ptBefore, ctBefore := p.promptToks, p.completionToks
	p.addUsage(body)

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
	recovered := false
	if len(msg.ToolCalls) == 0 {
		if tcs := parseContentToolCalls(msg.Content); len(tcs) > 0 {
			msg.ToolCalls = tcs
			msg.Content = ""
			recovered = true
		}
	}
	provDebugf("openai ChatTools <- HTTP 200 in %s (%d bytes) in_tok=%d out_tok=%d toolcalls=%d recovered=%t textlen=%d",
		elapsed, len(body), p.promptToks-ptBefore, p.completionToks-ctBefore,
		len(msg.ToolCalls), recovered, len(msg.Content))
	return msg, nil
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
