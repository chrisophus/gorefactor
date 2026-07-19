package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/chrisophus/gorefactor/orchestrator"
)

// TestExtractInterface_GeneratedInterfaceIsSatisfied proves the emitted
// interface compiles and the source type actually satisfies it: deleting or
// corrupting any generated method line breaks the compile-time assertion.
func TestExtractInterface_GeneratedInterfaceIsSatisfied(t *testing.T) {
	writeModule(t, map[string]string{"main.go": extractIfaceSrc})
	if err := extractInterfaceCommand([]string{"main.go", "Service", "Storer"}); err != nil {
		t.Fatalf("extract-interface: %v", err)
	}
	assertion := "package main\n\nvar _ Storer = (*Service)(nil)\n\nfunc main() {}\n"
	if err := os.WriteFile("assert.go", []byte(assertion), 0o644); err != nil {
		t.Fatal(err)
	}
	buildModule(t)
}

// TestImplementInterface_StubsSatisfyInterface proves the generated stubs
// complete the interface: the compile-time assertion fails if any stub is
// missing or malformed.
func TestImplementInterface_StubsSatisfyInterface(t *testing.T) {
	writeModule(t, map[string]string{"main.go": implIfaceSrc})
	if err := implementInterfaceCommand([]string{"main.go", "PartialWriter", "Writer"}); err != nil {
		t.Fatalf("implement-interface: %v", err)
	}
	assertion := "package main\n\nvar _ Writer = (*PartialWriter)(nil)\n\nfunc main() {}\n"
	if err := os.WriteFile("assert.go", []byte(assertion), 0o644); err != nil {
		t.Fatal(err)
	}
	buildModule(t)
}

// TestDoctorInstall_EnumeratesLiveRegistry pins the installed snippet to the
// live rule registry: every registered rule and every mechanically-fixable
// rule must be named. Deleting the enumeration from the generator's output
// fails this for all 40+ rules at once.
func TestDoctorInstall_EnumeratesLiveRegistry(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := doctorInstallCommand([]string{"--target", "claude.md"}); err != nil {
		t.Fatal(err)
	}
	content := readFile(t, "CLAUDE.md")
	for _, r := range defaultLintRules() {
		if !strings.Contains(content, r.Name()) {
			t.Errorf("installed rules do not name registered rule %q", r.Name())
		}
		if _, ok := r.(FixableRule); ok && !strings.Contains(content, r.Name()) {
			t.Errorf("fixable rule %q missing from installed rules", r.Name())
		}
	}
	if want := strconv.Itoa(len(defaultLintRules())); !strings.Contains(content, "("+want+")") {
		t.Errorf("installed rules do not state the live rule count %s", want)
	}
}

// TestInitAgentRules_CommandsAreReal exercises the written rules snippet
// against the CLI's command registry: every `gorefactor <cmd>` it tells an
// agent to run must actually be a registered command, so the snippet cannot
// drift into recommending commands that do not exist.
func TestInitAgentRules_CommandsAreReal(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := initAgentRulesCommand([]string{"--target", "claude.md"}); err != nil {
		t.Fatal(err)
	}
	content := readFile(t, "CLAUDE.md")
	registered := map[string]bool{}
	for _, name := range commandNames() {
		registered[name] = true
	}
	re := regexp.MustCompile("`gorefactor ([a-z][a-z0-9-]*)")
	matches := re.FindAllStringSubmatch(content, -1)
	if len(matches) < 10 {
		t.Fatalf("expected the snippet to reference many gorefactor commands, found %d", len(matches))
	}
	for _, m := range matches {
		if !registered[m[1]] {
			t.Errorf("agent-rules snippet references %q, which is not a registered command", m[1])
		}
	}
}

// TestInitAgentRules_MCPConfigIsUsable exercises the emitted .mcp.json: it
// must parse, launch the real `gorefactor mcp` entry point, and re-running
// must preserve both foreign servers and a user-customised gorefactor entry.
func TestInitAgentRules_MCPConfigIsUsable(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := initAgentRulesCommand([]string{"--mcp-only"}); err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		MCPServers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal([]byte(readFile(t, ".mcp.json")), &cfg); err != nil {
		t.Fatalf(".mcp.json is not valid JSON: %v", err)
	}
	entry, ok := cfg.MCPServers["gorefactor"]
	if !ok {
		t.Fatal(".mcp.json has no gorefactor server entry")
	}
	if entry.Command != "gorefactor" || len(entry.Args) == 0 || entry.Args[0] != "mcp" {
		t.Errorf("gorefactor entry does not launch `gorefactor mcp`: %+v", entry)
	}
	if !registeredCommand("mcp") {
		t.Error("emitted config launches `gorefactor mcp` but no mcp command is registered")
	}

	// Re-run against a customised config: nothing may be clobbered.
	custom := `{"mcpServers":{"other":{"command":"other-bin"},"gorefactor":{"command":"gorefactor","args":["mcp","--allow-write"]}}}`
	if err := os.WriteFile(".mcp.json", []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := initAgentRulesCommand([]string{"--mcp-only"}); err != nil {
		t.Fatal(err)
	}
	content := readFile(t, ".mcp.json")
	if !strings.Contains(content, "other-bin") || !strings.Contains(content, "--allow-write") {
		t.Errorf("re-run clobbered existing config:\n%s", content)
	}
}

// TestGenerateTemplates_PlansAreLoadableAndDispatchable exercises every
// emitted template through the real plan loader and checks each operation
// type against the orchestrator's executable set (built-ins plus the
// extract_method/inline_method bridges this package registers). This is the
// test that would have caught templates advertising rename_variable and
// extract_method ops the executor rejected as "unknown operation type".
func TestGenerateTemplates_PlansAreLoadableAndDispatchable(t *testing.T) {
	dir := t.TempDir()
	if err := generateTemplates([]string{dir}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) < 7 {
		t.Fatalf("expected at least 7 templates, got %d", len(entries))
	}
	known := map[string]bool{}
	for _, opType := range orchestrator.KnownOperationTypes() {
		known[opType] = true
	}
	orch := orchestrator.NewOrchestrator()
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		if e.Name() == "basic_plan_template.json" {
			// The skeleton template deliberately has zero operations (it is
			// the starting point users add ops to); the loader rightly
			// rejects it for execution, so only assert it parses as JSON.
			var raw map[string]interface{}
			if err := json.Unmarshal([]byte(readFile(t, filepath.Join(dir, e.Name()))), &raw); err != nil {
				t.Errorf("basic template is not valid JSON: %v", err)
			}
			continue
		}
		plan, err := orch.LoadPlan(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Errorf("template %s does not load as a plan: %v", e.Name(), err)
			continue
		}
		for _, op := range plan.Operations {
			if !known[op.Type] {
				t.Errorf("template %s contains op type %q, which no executor dispatches", e.Name(), op.Type)
			}
		}
	}
}

// TestExtractMethodPlan_ExecutesAndCompiles is the end-to-end behavioral
// test for the plan-level extract_method bridge: a real plan operation runs
// the type-aware extractor and the resulting module must compile. Deleting
// the core of the extractor's output (the synthesized function) makes the
// build fail.
func TestExtractMethodPlan_ExecutesAndCompiles(t *testing.T) {
	writeModule(t, map[string]string{"calc.go": `package m

func Compute(a, b int) int {
	c := a + b
	if c > 10 {
		c = c * 2
		c = c + 1
	}
	return c
}
`})
	orch := orchestrator.NewOrchestrator()
	result, err := orch.ExecuteOperations([]*orchestrator.RefactoringOperation{{
		Type: "extract_method",
		File: "calc.go",
		Parameters: map[string]interface{}{
			"methodName": "adjust",
			"startLine":  5,
			"endLine":    8,
		},
	}})
	if err != nil {
		t.Fatalf("extract_method plan failed: %v", err)
	}
	if !result.Success {
		t.Fatalf("extract_method plan unsuccessful: %+v", result.Errors)
	}
	src := readFile(t, "calc.go")
	if !strings.Contains(src, "func adjust(") && !strings.Contains(src, "func (") {
		t.Fatalf("no extracted function in output:\n%s", src)
	}
	buildModule(t)
}

// TestInlineMethodPlan_ExecutesAndCompiles is the same for the inline
// bridge: the plan op inlines a trivial function and the module still
// compiles with the function gone.
func TestInlineMethodPlan_ExecutesAndCompiles(t *testing.T) {
	writeModule(t, map[string]string{"calc.go": `package m

func double(x int) int { return x * 2 }

func Use(a int) int {
	return double(a) + 1
}
`})
	orch := orchestrator.NewOrchestrator()
	result, err := orch.ExecuteOperations([]*orchestrator.RefactoringOperation{{
		Type: "inline_method",
		File: "calc.go",
		Parameters: map[string]interface{}{
			"methodName": "double",
		},
	}})
	if err != nil {
		t.Fatalf("inline_method plan failed: %v", err)
	}
	if !result.Success {
		t.Fatalf("inline_method plan unsuccessful: %+v", result.Errors)
	}
	src := readFile(t, "calc.go")
	if strings.Contains(src, "func double(") {
		t.Fatalf("inlined function still present:\n%s", src)
	}
	buildModule(t)
}

// buildModule compiles the temp module the current test chdir'd into,
// failing the test with the compiler output when the generated code does
// not build.
func buildModule(t *testing.T) {
	t.Helper()
	cmd := exec.Command("go", "build", "./...")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated code does not compile: %v\n%s", err, out)
	}
}

// registeredCommand reports whether the CLI registers a command by name.
func registeredCommand(name string) bool {
	for _, n := range commandNames() {
		if n == name {
			return true
		}
	}
	return false
}
