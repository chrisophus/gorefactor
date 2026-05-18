package main

import (
	"encoding/json"
	"strings"
)

// allowedOps is the exact, closed vocabulary the model may emit. It
// matches orchestrator.dispatchOperation's switch -- nothing else will
// execute, so we constrain the model up front (harness "guide":
// variety reduction) rather than discovering it at apply time.
var allowedOps = []string{
	"rename_declaration",
	"replace_code",
	"move_method",
	"insert_code",
	"create_file",
	"delete_declaration",
	"remove_code_block",
}

// planJSONSchema is the decode-time grammar. It pins "type" to the
// allowed enum and forbids unknown operation-level keys, so a cheap
// model physically cannot emit create-file/path/content -- extras are
// forced to nest under the open "parameters" object. This eliminates
// the field/enum near-miss class at the source rather than repairing
// it after the fact. Parameters/target stay open: per-op param shapes
// are too varied to grammar-constrain without brittleness, and the
// canonicalizer + validator + build/test gate remain the backstops.
func planJSONSchema() string {
	enum := make([]string, len(allowedOps))
	copy(enum, allowedOps)
	schema := map[string]any{
		"type":                 "object",
		"required":             []string{"version", "name", "description", "operations"},
		"additionalProperties": false,
		"properties": map[string]any{
			"version":     map[string]any{"type": "string"},
			"name":        map[string]any{"type": "string"},
			"description": map[string]any{"type": "string"},
			"operations": map[string]any{
				"type":     "array",
				"minItems": 1,
				"items": map[string]any{
					"type":                 "object",
					"required":             []string{"type", "description", "file"},
					"additionalProperties": false,
					"properties": map[string]any{
						"type":        map[string]any{"type": "string", "enum": enum},
						"description": map[string]any{"type": "string"},
						"file":        map[string]any{"type": "string"},
						"target":      map[string]any{"type": "object"},
						"parameters":  map[string]any{"type": "object"},
					},
				},
			},
		},
	}
	b, _ := json.Marshal(schema)
	return string(b)
}

// systemPrompt is the feedforward guide. It pins the output to a single
// schema, forbids prose, forbids line-number targeting, and forbids
// behavioural change -- everything that keeps a cheap model on rails.
func systemPrompt() string {
	return `You are a mechanical Go refactoring planner. You do NOT write prose.
You emit exactly ONE JSON object: a gorefactor RefactoringPlan. No markdown
fences, no commentary, no explanation -- JSON only.

HARD RULES:
1. Output is a single JSON object and nothing else.
2. "operations[].type" MUST be one of: ` + strings.Join(allowedOps, ", ") + `.
3. Target code SEMANTICALLY via "target" (functionName, methodName,
   receiverType, typeName, constName, varName). NEVER use startLine/endLine.
4. Refactors MUST preserve behaviour. No logic, control-flow, or API
   semantic changes -- only structure/names.
5. Every operation needs "file" and (except insert_code/create_file) a
   non-empty "target".
6. The plan needs "version", "name" (kebab-case), "description", and a
   non-empty "operations" array.

SHAPE you MUST emit (this is a STRUCTURE illustration -- the strings
below are PLACEHOLDERS, never copy them; fill every field from the
ACTUAL task you are given):
{
  "version": "1.0",
  "name": "<kebab-case-name-for-this-task>",
  "description": "<what this specific plan does>",
  "operations": [
    { "type": "<one allowed type>", "description": "<...>",
      "file": "<real/path/in/this/repo.go>",
      "target": { "<selector>": "<real symbol>" },
      "parameters": { "<...>": "<...>" } }
  ]
}
ALWAYS emit this full object with an "operations" ARRAY -- never a
bare operation object, never a top-level array.

PARAMETERS BY TYPE:
- rename_declaration: target={functionName|methodName|typeName|constName|varName}; parameters.newName
- replace_code:       file; parameters.codePattern = a COMPLETE top-level
                      statement of the target function body (a whole
                      for/if/switch/assignment/return statement -- NEVER a
                      fragment or a nested line; whitespace is ignored when
                      matching), parameters.replacementCode = equivalent
                      replacement statement(s),
                      parameters.location = {"functionName":"F"} OR
                      {"methodName":"M","receiverType":"T"}
- move_method:        target={methodName,receiverType}; parameters.newFile
- insert_code:        file; parameters.codeSnippet;
                      parameters.location = {"type":"at_end"|"before_function"|
                      "after_function"|"inside_function"|"at_beginning",
                      "functionName":"F"}  (type is REQUIRED)
- create_file:        file (new path); parameters.codeSnippet (full file text)
- delete_declaration: target={functionName|methodName|typeName}
- remove_code_block:  file; parameters.codePattern

ADDING NEW CODE (first-class -- the spec may ask for new code, not
just transformations):
- New whole file  -> create_file. "file" is the new path;
  parameters.codeSnippet is the COMPLETE file contents including the
  package clause and any imports.
- New function/method/type in an existing file -> insert_code with
  parameters.location.type = "after_function" (anchor via
  location.functionName) or "at_end". parameters.codeSnippet is the
  full new declaration.
Example (add a new function after an existing one):
{ "type": "insert_code", "description": "add helper", "file": "x.go",
  "parameters": { "codeSnippet": "func helper() int { return 0 }",
  "location": { "type": "after_function", "functionName": "Sum" } } }

GUIDANCE:
- To CHANGE existing code, prefer rename_declaration and replace_code
  (safest, most deterministic).
- To ADD code, use create_file or insert_code as above.
- Never mix unrelated changes; keep the plan minimal and behaviour-safe.`
}

var specStopwords = map[string]bool{
	"the": true, "and": true, "for": true, "with": true, "into": true,
	"from": true, "that": true, "this": true, "code": true, "func": true,
	"function": true, "method": true, "file": true, "add": true, "new": true,
	"use": true, "rename": true, "extract": true, "move": true, "should": true,
}

// buildUserPrompt assembles the per-iteration request: the purified
// spec, the semantic code map, the actual source of spec-relevant
// files, and any structured failure from the previous attempt (the
// feedback sensor closing the loop).
func buildUserPrompt(spec, dir, feedback string) string {
	var b strings.Builder
	// Context first, instruction last: small models attend most to the
	// end of the prompt, so the actual task and the output directive go
	// last where they can't be drowned by the code map.
	b.WriteString("CODE MAP (target these symbols semantically):\n")
	b.WriteString(codeMap(dir))
	b.WriteString("\n\nRELEVANT SOURCE (the most spec-relevant files):\n")
	b.WriteString(relevantSource(spec, dir, 16000, 8000))
	if strings.TrimSpace(feedback) != "" {
		b.WriteString("\n\nYOUR PREVIOUS ATTEMPT FAILED. Fix it. Failure detail:\n")
		b.WriteString(strings.TrimSpace(feedback))
	}
	b.WriteString("\n\n════════ THE TASK ════════\n")
	b.WriteString("Do EXACTLY this, and nothing else:\n\n")
	b.WriteString(strings.TrimSpace(spec))
	b.WriteString("\n\nOutput ONE JSON object: a RefactoringPlan with " +
		"\"version\", \"name\", \"description\", and an \"operations\" " +
		"ARRAY containing the operation(s) for THIS task. Do NOT copy " +
		"the example's placeholder names/paths (oldFunction, " +
		"path/to/file.go, etc.) -- they are illustrative only. JSON only.")
	return b.String()
}
