package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

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

	status, body, err := p.doWithRetry(ctx, p.baseURL+"/chat/completions", buf)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("provider HTTP %d: %s", status, strings.TrimSpace(string(body)))
	}
	p.addUsage(body)

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
