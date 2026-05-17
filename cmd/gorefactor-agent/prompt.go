package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/chrisophus/gorefactor/parser"
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

// codeMap gives the model the semantic anchors it must target by:
// the functions/methods/types per .go file. Built from gorefactor's
// own parser so the map can never drift from what the engine sees.
func goFiles(dir string) []string {
	var files []string
	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			if d != nil && d.IsDir() {
				n := d.Name()
				if n == ".git" || n == "vendor" || n == "testdata" {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			files = append(files, path)
		}
		return nil
	})
	sort.Strings(files)
	return files
}

func codeMap(dir string) string {
	var b strings.Builder
	files := goFiles(dir)

	for _, f := range files {
		rel, _ := filepath.Rel(dir, f)
		info, err := parser.ParseFile(f)
		if err != nil {
			continue
		}
		b.WriteString(rel)
		b.WriteString(":\n")
		for _, fn := range info.Functions {
			fmt.Fprintf(&b, "  func %s\n", fn.Name)
		}
		for _, m := range info.Methods {
			fmt.Fprintf(&b, "  method %s.%s\n", m.Receiver, m.Name)
		}
	}
	if b.Len() == 0 {
		return "(no Go files found)"
	}
	return b.String()
}

var specStopwords = map[string]bool{
	"the": true, "and": true, "for": true, "with": true, "into": true,
	"from": true, "that": true, "this": true, "code": true, "func": true,
	"function": true, "method": true, "file": true, "add": true, "new": true,
	"use": true, "rename": true, "extract": true, "move": true, "should": true,
}

// specTokens pulls candidate identifiers/words out of a spec for
// matching against code. Deterministic, no NLP -- just enough signal
// to rank files.
func specTokens(spec string) []string {
	seen := map[string]bool{}
	var out []string
	cur := strings.Builder{}
	flush := func() {
		if cur.Len() == 0 {
			return
		}
		t := strings.ToLower(cur.String())
		cur.Reset()
		if len(t) < 3 || specStopwords[t] || seen[t] {
			return
		}
		seen[t] = true
		out = append(out, t)
	}
	for _, r := range spec {
		if r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			cur.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return out
}

// relevantSource is deterministic feedforward context: it ranks files
// by how well their path and symbol names match the spec, then inlines
// the actual source of the top matches within a byte budget. A cheap
// model can't target or write fitting code from a symbol map alone.
func relevantSource(spec, dir string, totalBudget, perFileCap int) string {
	tokens := specTokens(spec)
	if len(tokens) == 0 {
		return "(spec has no distinctive terms; rely on the code map)"
	}

	type scored struct {
		rel   string
		path  string
		score int
	}
	var ranked []scored
	for _, f := range goFiles(dir) {
		rel, _ := filepath.Rel(dir, f)
		relLower := strings.ToLower(rel)
		score := 0
		for _, t := range tokens {
			if strings.Contains(relLower, t) {
				score += 2
			}
		}
		if info, err := parser.ParseFile(f); err == nil {
			names := make([]string, 0, len(info.Functions)+len(info.Methods))
			for _, fn := range info.Functions {
				names = append(names, fn.Name)
			}
			for _, m := range info.Methods {
				names = append(names, m.Name)
			}
			for _, n := range names {
				nl := strings.ToLower(n)
				for _, t := range tokens {
					if strings.Contains(nl, t) || strings.Contains(t, nl) {
						score += 3
					}
				}
			}
		}
		if data, err := os.ReadFile(f); err == nil {
			cl := strings.ToLower(string(data))
			for _, t := range tokens {
				if c := strings.Count(cl, t); c > 0 {
					score += min(c, 3)
				}
			}
		}
		if score > 0 {
			ranked = append(ranked, scored{rel, f, score})
		}
	}
	if len(ranked) == 0 {
		return "(no files matched the spec terms; rely on the code map)"
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].rel < ranked[j].rel
	})

	var b strings.Builder
	used := 0
	for _, s := range ranked {
		if used >= totalBudget {
			break
		}
		data, err := os.ReadFile(s.path)
		if err != nil {
			continue
		}
		src := string(data)
		truncated := false
		if len(src) > perFileCap {
			src = src[:perFileCap]
			truncated = true
		}
		fmt.Fprintf(&b, "=== %s ===\n%s\n", s.rel, src)
		if truncated {
			b.WriteString("…(file truncated)\n")
		}
		used += len(src)
	}
	return strings.TrimRight(b.String(), "\n")
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
		// content-ish synonyms -> parameters.codeSnippet
		for _, k := range []string{"content", "code", "codeSnippet", "snippet", "body", "fileContent"} {
			if v, ok := op[k]; ok {
				if _, exists := params["codeSnippet"]; !exists {
					params["codeSnippet"] = v
				}
				delete(op, k)
			}
		}
		// known params placed at op top level -> parameters
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
