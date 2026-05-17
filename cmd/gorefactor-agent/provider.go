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

func (p *openAIProvider) Complete(ctx context.Context, system, user string) (string, error) {
	reqBody := map[string]any{
		"model": p.model,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
		"temperature": 0,
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
