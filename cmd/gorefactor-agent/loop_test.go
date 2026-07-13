package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const sampleGo = `package sample

func Sum(nums []int) int {
	total := 0
	for i := 0; i < len(nums); i++ {
		total = total + nums[i]
	}
	return total
}
`

const sampleTestGo = `package sample

import "testing"

func TestSum(t *testing.T) {
	if Sum([]int{1, 2, 3}) != 6 {
		t.Fatalf("Sum wrong")
	}
}
`

// a behaviour-preserving replace_code plan: replace the WHOLE top-level
// for-statement in Sum with an equivalent range loop. replace_code
// swaps a complete top-level statement of the function body, so the
// pattern must be the entire for-statement, not a fragment of it.
const goodPlan = `{
  "version": "1.0",
  "name": "simplify-sum",
  "description": "use a range loop in Sum",
  "operations": [
    {
      "type": "replace_code",
      "description": "range loop",
      "file": "sample.go",
      "target": { "functionName": "Sum" },
      "parameters": {
        "location": { "functionName": "Sum" },
        "codePattern": "for i := 0; i < len(nums); i++ { total = total + nums[i] }",
        "replacementCode": "for _, n := range nums { total += n }"
      }
    }
  ]
}`

func TestDriver_AppliesAndGatesPassingRefactor(t *testing.T) {
	dir := newSampleRepo(t)

	var log bytes.Buffer
	err := RunDriver(context.Background(),
		&mockProvider{responses: []string{goodPlan}},
		Config{Spec: "use += in Sum", Dir: dir, MaxIter: 2, Out: &log})
	if err != nil {
		t.Fatalf("RunDriver: %v\nlog:\n%s", err, log.String())
	}

	got, _ := os.ReadFile(filepath.Join(dir, "sample.go"))
	if !strings.Contains(string(got), "range nums") {
		t.Fatalf("refactor not applied; file:\n%s", got)
	}
	if strings.Contains(string(got), "i < len(nums)") {
		t.Fatalf("old loop still present; file:\n%s", got)
	}
}

func TestDriver_RecoversFromBadFirstResponse(t *testing.T) {
	dir := newSampleRepo(t)

	mock := &mockProvider{responses: []string{
		"sorry, I cannot help with that", // not JSON -> must retry
		goodPlan,                         // valid on second try
	}}

	var log bytes.Buffer
	err := RunDriver(context.Background(), mock,
		Config{Spec: "use += in Sum", Dir: dir, MaxIter: 3, Out: &log})
	if err != nil {
		t.Fatalf("RunDriver should recover: %v\nlog:\n%s", err, log.String())
	}
	if mock.calls != 2 {
		t.Fatalf("expected 2 provider calls (1 bad, 1 good), got %d", mock.calls)
	}
}

func TestDriver_CreatesNewFile(t *testing.T) {
	dir := newSampleRepo(t)

	var log bytes.Buffer
	err := RunDriver(context.Background(),
		&mockProvider{responses: []string{createFilePlan}},
		Config{Spec: "add file mathx.go with Double", Dir: dir, MaxIter: 2, Out: &log})
	if err != nil {
		t.Fatalf("RunDriver: %v\nlog:\n%s", err, log.String())
	}
	got, err := os.ReadFile(filepath.Join(dir, "mathx.go"))
	if err != nil {
		t.Fatalf("new file not created: %v", err)
	}
	if !strings.Contains(string(got), "func Double(n int) int") {
		t.Fatalf("new file missing expected code:\n%s", got)
	}
}

// additive: create a brand-new file in the module.
const createFilePlan = `{
  "version": "1.0",
  "name": "add-mathx",
  "description": "add mathx.go with Double",
  "operations": [
    {
      "type": "create_file",
      "description": "new file",
      "file": "mathx.go",
      "parameters": {
        "codeSnippet": "package sample\n\nfunc Double(n int) int { return n * 2 }\n"
      }
    }
  ]
}`

func TestDriver_InsertsNewFunction(t *testing.T) {
	dir := newSampleRepo(t)

	var log bytes.Buffer
	err := RunDriver(context.Background(),
		&mockProvider{responses: []string{insertFuncPlan}},
		Config{Spec: "add a Triple function after Sum", Dir: dir, MaxIter: 2, Out: &log})
	if err != nil {
		t.Fatalf("RunDriver: %v\nlog:\n%s", err, log.String())
	}
	got, _ := os.ReadFile(filepath.Join(dir, "sample.go"))
	if !strings.Contains(string(got), "func Triple(n int) int") {
		t.Fatalf("inserted function missing:\n%s", got)
	}
	if !strings.Contains(string(got), "func Sum(") {
		t.Fatalf("insert clobbered existing code:\n%s", got)
	}
}

// additive: insert a new top-level function after an existing one.
const insertFuncPlan = `{
  "version": "1.0",
  "name": "add-triple",
  "description": "insert Triple after Sum",
  "operations": [
    {
      "type": "insert_code",
      "description": "new function",
      "file": "sample.go",
      "parameters": {
        "codeSnippet": "func Triple(n int) int { return n * 3 }",
        "location": { "type": "after_function", "functionName": "Sum" }
      }
    }
  ]
}`

func TestDriver_DryRunDoesNotModify(t *testing.T) {
	dir := newSampleRepo(t)
	before, _ := os.ReadFile(filepath.Join(dir, "sample.go"))

	var log bytes.Buffer
	err := RunDriver(context.Background(),
		&mockProvider{responses: []string{goodPlan}},
		Config{Spec: "use += in Sum", Dir: dir, MaxIter: 1, DryRun: true, Out: &log})
	if err != nil {
		t.Fatalf("dry-run: %v\nlog:\n%s", err, log.String())
	}
	after, _ := os.ReadFile(filepath.Join(dir, "sample.go"))
	if string(before) != string(after) {
		t.Fatalf("dry-run modified the file")
	}
}

func newSampleRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module sample\n\ngo 1.21\n")
	write("sample.go", sampleGo)
	write("sample_test.go", sampleTestGo)
	// Mirror the real repo: .gorefactor/ is gitignored, so the agent's
	// rollback (git clean -fd, no -x) preserves the persistent notes and
	// failure corpus across attempts.
	write(".gitignore", ".gorefactor/\n")

	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "t"},
		{"config", "commit.gpgsign", "false"},
		{"add", "-A"},
		{"commit", "-q", "-m", "init"},
	} {
		c := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}
