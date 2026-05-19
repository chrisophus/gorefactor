package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
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

	status, body, err := p.doWithRetry(ctx, p.baseURL+"/v1/messages", buf)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("anthropic HTTP %d: %s", status, strings.TrimSpace(string(body)))
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

func (p *anthropicProvider) ChatTools(ctx context.Context, messages []chatMessage, tools []toolDef) (chatMessage, error) {

	var systemParts []string
	type anthMsg struct {
		role   string
		blocks []map[string]any
	}
	var conv []anthMsg

	appendBlock := func(role string, block map[string]any) {
		if n := len(conv); n > 0 && conv[n-1].role == role {
			conv[n-1].blocks = append(conv[n-1].blocks, block)
			return
		}
		conv = append(conv, anthMsg{role: role, blocks: []map[string]any{block}})
	}

	for _, m := range messages {
		switch m.Role {
		case "system":
			if strings.TrimSpace(m.Content) != "" {
				systemParts = append(systemParts, m.Content)
			}
		case "tool":
			appendBlock("user", map[string]any{
				"type":        "tool_result",
				"tool_use_id": m.ToolCallID,
				"content":     m.Content,
			})
		case "assistant":
			if strings.TrimSpace(m.Content) != "" {
				appendBlock("assistant", map[string]any{"type": "text", "text": m.Content})
			}
			for _, tc := range m.ToolCalls {
				var input any = map[string]any{}
				if strings.TrimSpace(tc.Function.Arguments) != "" {
					_ = json.Unmarshal([]byte(tc.Function.Arguments), &input)
				}
				appendBlock("assistant", map[string]any{
					"type":  "tool_use",
					"id":    tc.ID,
					"name":  tc.Function.Name,
					"input": input,
				})
			}
		default:
			if strings.TrimSpace(m.Content) != "" {
				appendBlock("user", map[string]any{"type": "text", "text": m.Content})
			}
		}
	}

	apiMsgs := make([]map[string]any, 0, len(conv))
	for _, c := range conv {
		apiMsgs = append(apiMsgs, map[string]any{"role": c.role, "content": c.blocks})
	}

	reqBody := map[string]any{
		"model":       p.model,
		"max_tokens":  p.maxTokens,
		"messages":    apiMsgs,
		"temperature": 0,
	}
	if s := strings.TrimSpace(strings.Join(systemParts, "\n\n")); s != "" {
		reqBody["system"] = s
	}
	if len(tools) > 0 {
		at := make([]map[string]any, 0, len(tools))
		for _, t := range tools {
			schema := t.Function.Parameters
			if schema == nil {
				schema = map[string]any{"type": "object", "properties": map[string]any{}}
			}
			at = append(at, map[string]any{
				"name":         t.Function.Name,
				"description":  t.Function.Description,
				"input_schema": schema,
			})
		}
		reqBody["tools"] = at
	}

	buf, err := json.Marshal(reqBody)
	if err != nil {
		return chatMessage{}, err
	}
	endpoint := p.baseURL + "/v1/messages"
	provDebugf("anthropic ChatTools -> POST %s model=%s msgs=%d tools=%d reqBytes=%d",
		endpoint, p.model, len(apiMsgs), len(tools), len(buf))

	start := time.Now()
	status, body, err := p.doWithRetry(ctx, endpoint, buf)
	elapsed := time.Since(start)
	if err != nil {
		provDebugf("anthropic ChatTools FAILED after %s: %v", elapsed, err)
		return chatMessage{}, err
	}
	if status != http.StatusOK {
		provDebugf("anthropic ChatTools <- HTTP %d in %s (%d bytes): %s",
			status, elapsed, len(body), strings.TrimSpace(string(body)))
		return chatMessage{}, fmt.Errorf("anthropic HTTP %d: %s", status, strings.TrimSpace(string(body)))
	}

	var parsed struct {
		Content []struct {
			Type  string          `json:"type"`
			Text  string          `json:"text"`
			ID    string          `json:"id"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
		Usage      struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return chatMessage{}, fmt.Errorf("decode anthropic tool response: %w", err)
	}
	p.promptToks += parsed.Usage.InputTokens
	p.completionToks += parsed.Usage.OutputTokens

	out := chatMessage{Role: "assistant"}
	var text strings.Builder
	for _, c := range parsed.Content {
		switch c.Type {
		case "text":
			text.WriteString(c.Text)
		case "tool_use":
			args := string(c.Input)
			if strings.TrimSpace(args) == "" {
				args = "{}"
			}
			var call toolCall
			call.ID = c.ID
			call.Type = "function"
			call.Function.Name = c.Name
			call.Function.Arguments = args
			out.ToolCalls = append(out.ToolCalls, call)
		}
	}
	out.Content = text.String()
	provDebugf("anthropic ChatTools <- HTTP 200 in %s (%d bytes) stop=%s in_tok=%d out_tok=%d toolcalls=%d textlen=%d",
		elapsed, len(body), parsed.StopReason, parsed.Usage.InputTokens, parsed.Usage.OutputTokens,
		len(out.ToolCalls), len(out.Content))
	return out, nil
}

func (p *anthropicProvider) doWithRetry(ctx context.Context, endpoint string, buf []byte) (int, []byte, error) {
	const maxAttempts = 5
	var lastErr error
	var prevResp *http.Response // headers only after attempts 1..N-1; carries Retry-After
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			delay := retryDelay(attempt, prevResp)
			provDebugf("anthropic retry %d/%d after %s backoff (last: %v)",
				attempt+1, maxAttempts, delay, lastErr)
			if err := retrySleep(ctx, delay); err != nil {
				return 0, nil, err
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(buf))
		if err != nil {
			return 0, nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-api-key", p.apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")
		resp, err := p.client.Do(req)
		if err != nil {
			lastErr = err
			prevResp = nil
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("anthropic HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
			prevResp = resp
			continue
		}
		return resp.StatusCode, body, nil
	}
	return 0, nil, fmt.Errorf("anthropic request failed after %d attempts: %w", maxAttempts, lastErr)
}

var retrySleep = func(ctx context.Context, d time.Duration) error {
	select {
	case <-time.After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func retryDelay(attempt int, prevResp *http.Response) time.Duration {
	const cap = 30 * time.Second
	clamp := func(d time.Duration) time.Duration {
		if d > cap {
			return cap
		}
		return d
	}
	if prevResp != nil {
		if h := prevResp.Header.Get("Retry-After"); h != "" {
			if secs, err := strconv.Atoi(h); err == nil && secs > 0 {
				return clamp(time.Duration(secs) * time.Second)
			}
		}
	}
	return clamp(time.Duration(1<<uint(attempt-1)) * time.Second)
}
