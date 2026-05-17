package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

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
