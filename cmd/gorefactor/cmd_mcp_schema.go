package main

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// toolInputSchema derives a JSON Schema for a command's parameters: an `args`
// array for positional arguments (bounded by MinArgs/MaxArgs) plus one
// property per exposed flag (boolean flags -> boolean, value flags -> string).
// It is returned as json.RawMessage so the SDK passes it through verbatim
// (Server.AddTool does no schema inference or validation of its own).
func toolInputSchema(cmd Command) json.RawMessage {
	properties := map[string]interface{}{}
	var required []string

	if cmd.MaxArgs != 0 {
		argsSchema := map[string]interface{}{
			"type":        "array",
			"items":       map[string]interface{}{"type": "string"},
			"description": "Positional arguments, in order. See Usage in the tool description.",
		}
		if cmd.MinArgs > 0 {
			argsSchema["minItems"] = cmd.MinArgs
			required = append(required, "args")
		}
		if cmd.MaxArgs > 0 {
			argsSchema["maxItems"] = cmd.MaxArgs
		}
		properties["args"] = argsSchema
	}

	for _, flag := range sortedFlagNames(cmd.Flags) {
		if mcpSkipFlags[flag] {
			continue
		}
		key := flagParamName(flag)
		if cmd.Flags[flag] {
			properties[key] = map[string]interface{}{
				"type":        "string",
				"description": "Value for the " + flag + " flag.",
			}
		} else {
			properties[key] = map[string]interface{}{
				"type":        "boolean",
				"description": "Enable the " + flag + " flag.",
			}
		}
	}

	schema := map[string]interface{}{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	raw, err := json.Marshal(schema)
	if err != nil {
		// The schema is built from string keys and primitive values, so this
		// cannot fail in practice; fall back to a permissive object schema.
		return json.RawMessage(`{"type":"object"}`)
	}
	return raw
}

// flagParamName converts a CLI flag (e.g. "--in") into a JSON property name
// (e.g. "in"). JSON property names can't begin with a dash for many clients,
// and the leading dashes are noise in a structured schema.
func flagParamName(flag string) string {
	i := 0
	for i < len(flag) && flag[i] == '-' {
		i++
	}
	return flag[i:]
}

func sortedFlagNames(flags map[string]bool) []string {
	names := make([]string, 0, len(flags))
	for name := range flags {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// buildArgv turns the tool arguments object into the positional + flag argv
// the command expects, forcing --json when the command supports it so clients
// always receive structured output.
func buildArgv(cmd Command, raw json.RawMessage) ([]string, error) {
	args := map[string]json.RawMessage{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, fmt.Errorf("invalid arguments object: %w", err)
		}
	}

	var argv []string
	if v, ok := args["args"]; ok {
		var positional []string
		if err := json.Unmarshal(v, &positional); err != nil {
			return nil, fmt.Errorf("\"args\" must be an array of strings: %w", err)
		}
		argv = append(argv, positional...)
	}

	for _, flag := range sortedFlagNames(cmd.Flags) {
		if mcpSkipFlags[flag] {
			continue
		}
		key := flagParamName(flag)
		v, ok := args[key]
		if !ok {
			continue
		}
		if cmd.Flags[flag] {
			var val string
			if err := json.Unmarshal(v, &val); err != nil {
				return nil, fmt.Errorf("%q must be a string", key)
			}
			argv = append(argv, flag, val)
		} else {
			var on bool
			if err := json.Unmarshal(v, &on); err != nil {
				return nil, fmt.Errorf("%q must be a boolean", key)
			}
			if on {
				argv = append(argv, flag)
			}
		}
	}

	if _, supportsJSON := cmd.Flags["--json"]; supportsJSON {
		argv = append(argv, "--json")
	}

	if err := checkCommandArgs(cmd, argv); err != nil {
		return nil, err
	}
	return argv, nil
}

func toolTextResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}
}

func toolErrorResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
		IsError: true,
	}
}

func toolDescription(cmd Command) string {
	desc := cmd.Description
	if cmd.Usage != "" {
		desc += "\n\nUsage: gorefactor " + cmd.Usage
	}
	return desc
}
