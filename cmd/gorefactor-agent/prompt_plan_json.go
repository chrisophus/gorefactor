package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// extractPlanJSON pulls the first balanced top-level JSON object out of
// a model response, tolerating stray prose or ``` fences a cheap model
// may still leak despite instructions.
func extractPlanJSON(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")

	// Start at the first JSON container, object or array -- cheap
	// models sometimes return a top-level array of operations.
	ob := strings.IndexByte(s, '{')
	br := strings.IndexByte(s, '[')
	start := ob
	if br >= 0 && (ob < 0 || br < ob) {
		start = br
	}
	if start < 0 {
		return "", fmt.Errorf("no JSON value found in model output")
	}

	depth := 0
	inStr := false
	esc := false
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
		case c == '{', c == '[':
			depth++
		case c == '}', c == ']':
			depth--
			if depth == 0 {
				return s[start : i+1], nil
			}
		}
	}
	return "", fmt.Errorf("unbalanced JSON value in model output")
}

// normalizeToPlanJSON reshapes near-misses into a valid plan: a bare
// operation object, a top-level array of operations, or a plan whose
// "operations" is a single object instead of an array. Cheap models
// produce these constantly; normalizing here is far cheaper than a
// retry round-trip.
func normalizeToPlanJSON(js string) (string, error) {
	t := strings.TrimSpace(js)

	wrap := func(opsArray string) (string, error) {
		plan := map[string]any{
			"version":     "1.0",
			"name":        "auto",
			"description": "auto-wrapped operations",
			"operations":  json.RawMessage(opsArray),
		}
		out, err := json.Marshal(plan)
		return string(out), err
	}

	if strings.HasPrefix(t, "[") {
		return wrap(t)
	}

	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(t), &top); err != nil {
		return "", err
	}
	if ops, ok := top["operations"]; ok {
		if strings.HasPrefix(strings.TrimSpace(string(ops)), "{") {
			top["operations"] = json.RawMessage("[" + string(ops) + "]")
			out, err := json.Marshal(top)
			return string(out), err
		}
		return t, nil
	}
	if _, ok := top["type"]; ok {
		return wrap("[" + t + "]")
	}
	return t, nil
}

// canonicalizePlanJSON absorbs the field/enum near-misses cheap models
// reliably make: hyphenated/cased op types, "path" for "file", and
// content/param keys placed at the operation top level instead of
// under "parameters". Deterministic glue is far cheaper than a retry.
func canonicalizePlanJSON(js string) (string, error) {
	var plan map[string]any
	if err := json.Unmarshal([]byte(js), &plan); err != nil {
		return "", err
	}
	ops, _ := plan["operations"].([]any)
	for _, o := range ops {
		op, ok := o.(map[string]any)
		if !ok {
			continue
		}
		if t, ok := op["type"].(string); ok {
			op["type"] = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(t), "-", "_"))
		}
		if _, has := op["file"]; !has {
			for _, k := range []string{"path", "filename", "filePath"} {
				if v, ok := op[k].(string); ok {
					op["file"] = v
					delete(op, k)
					break
				}
			}
		}
		params, _ := op["parameters"].(map[string]any)
		if params == nil {
			params = map[string]any{}
		}

		for _, k := range []string{"content", "code", "codeSnippet", "snippet", "body", "fileContent"} {
			if v, ok := op[k]; ok {
				if _, exists := params["codeSnippet"]; !exists {
					params["codeSnippet"] = v
				}
				delete(op, k)
			}
		}

		for _, k := range []string{"newName", "replacementCode", "codePattern", "newFile", "location"} {
			if v, ok := op[k]; ok {
				if _, exists := params[k]; !exists {
					params[k] = v
				}
				delete(op, k)
			}
		}
		if len(params) > 0 {
			op["parameters"] = params
		}
	}
	out, err := json.Marshal(plan)
	return string(out), err
}
