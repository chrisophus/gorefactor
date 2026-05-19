package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// anthropicProvider talks to the native Anthropic Messages API. It is
// separate from openAIProvider because Anthropic's wire format differs
// (x-api-key header, anthropic-version, top-level system, content
// blocks). This lets dogfooding run directly on a cheap Claude model
// such as Haiku without an OpenAI-compat gateway.
type anthropicProvider struct {
	baseURL   string
	apiKey    string
	model     string
	maxTokens int
	client    *http.Client

	promptToks, completionToks int
}

func (p *anthropicProvider) Tokens() (int, int) { return p.promptToks, p.completionToks }

func newAnthropicProvider(baseURL, apiKey, model string) *anthropicProvider {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	if model == "" {
		model = "claude-haiku-4-5-20251001"
	}
	return &anthropicProvider{
		baseURL:   strings.TrimRight(baseURL, "/"),
		apiKey:    apiKey,
		model:     model,
		maxTokens: 4096,
		client:    &http.Client{Timeout: 120 * time.Second},
	}
}

func (p *anthropicProvider) Complete(ctx context.Context, system, user string) (string, error) {
	reqBody := map[string]any{
		"model":      p.model,
		"max_tokens": p.maxTokens,
		"system":     system,
		"messages": []map[string]any{
			{"role": "user", "content": user},
		},
		"temperature": 0,
	}
	buf, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.baseURL+"/v1/messages", bytes.NewReader(buf))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("anthropic HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var parsed struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("decode anthropic response: %w", err)
	}
	p.promptToks += parsed.Usage.InputTokens
	p.completionToks += parsed.Usage.OutputTokens
	var sb strings.Builder
	for _, c := range parsed.Content {
		if c.Type == "text" {
			sb.WriteString(c.Text)
		}
	}
	if sb.Len() == 0 {
		return "", fmt.Errorf("anthropic returned no text content")
	}
	return sb.String(), nil
}
