package main

import (
	"context"
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
