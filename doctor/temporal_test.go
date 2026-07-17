package doctor

import (
	"strings"
	"testing"
)

func TestTemporalSubstrate_FlagsViolations(t *testing.T) {
	dir := writeTemporalModule(t, violatingWorkflow)
	findings, err := Temporal{}.Run(RunContext{Root: dir})
	if err != nil {
		t.Fatal(err)
	}
	rules := map[string]int{}
	for _, f := range findings {
		rules[f.Rule]++
		if f.Category != CategoryTemporal {
			t.Errorf("finding %s category = %s, want tmprl", f.Rule, f.Category)
		}
		if !strings.Contains(f.Message, "MyWorkflow") {
			t.Errorf("message should name the workflow: %q", f.Message)
		}
	}
	// time.Now appears twice (once in the goroutine body — replay executes
	// nested literals too).
	for rule, want := range map[string]int{
		"temporal/time":      3,
		"temporal/rand":      1,
		"temporal/goroutine": 1,
		"temporal/channel":   1,
		"temporal/select":    1,
	} {
		if rules[rule] != want {
			t.Errorf("rule %s count = %d, want %d (all: %v)", rule, rules[rule], want, rules)
		}
	}
}

const violatingWorkflow = `package wf

import (
	"math/rand"
	"time"

	"go.temporal.io/sdk/workflow"
)

func MyWorkflow(ctx workflow.Context) error {
	now := time.Now()
	_ = now
	time.Sleep(time.Second)
	_ = rand.Intn(10)
	go func() { _ = time.Now() }()
	ch := make(chan int)
	select {
	case <-ch:
	default:
	}
	return nil
}

func helper(n int) int { return n + 1 }
`

func TestTemporalSubstrate_IgnoresNonWorkflowFunctions(t *testing.T) {
	src := `package wf

import (
	"context"
	"time"
)

func NotAWorkflow(ctx context.Context) time.Time { return time.Now() }
`
	dir := writeTemporalModule(t, src)
	findings, err := Temporal{}.Run(RunContext{Root: dir})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("functions without workflow.Context are out of scope: %+v", findings)
	}
}

func TestTemporalSubstrate_NoTemporalDependency(t *testing.T) {
	dir := t.TempDir()
	writeShapeFile(t, dir, "go.mod", "module example.com/plain\n\ngo 1.26\n")
	writeShapeFile(t, dir, "a.go", "package a\n\nimport \"time\"\n\nfunc Now() time.Time { return time.Now() }\n")
	findings, err := Temporal{}.Run(RunContext{Root: dir})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("module without temporal dep must trivially pass: %+v", findings)
	}
}

func TestTemporalSubstrate_RenamedImports(t *testing.T) {
	src := `package wf

import (
	wf "go.temporal.io/sdk/workflow"
	clock "time"
)

func Renamed(ctx wf.Context) error {
	_ = clock.Now()
	return nil
}
`
	dir := writeTemporalModule(t, src)
	findings, err := Temporal{}.Run(RunContext{Root: dir})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].Rule != "temporal/time" {
		t.Fatalf("renamed imports must still match: %+v", findings)
	}
}

func TestParseWorkflowcheckOutput(t *testing.T) {
	out := []byte(`workflowcheck: some banner
/root/wf/workflow.go:12:2: MyWorkflow is non-deterministic, reason: calls time.Now
wf/other.go:7: OtherWorkflow is non-deterministic
not a diagnostic line
`)
	findings := parseWorkflowcheckOutput(out, "/root")
	if len(findings) != 2 {
		t.Fatalf("findings = %+v, want 2", findings)
	}
	if findings[0].File != "wf/workflow.go" || findings[0].Line != 12 {
		t.Errorf("finding 0 = %s:%d", findings[0].File, findings[0].Line)
	}
	if findings[1].File != "wf/other.go" || findings[1].Line != 7 {
		t.Errorf("finding 1 = %s:%d", findings[1].File, findings[1].Line)
	}
	for _, f := range findings {
		if f.Category != CategoryTemporal || f.Rule != "temporal/workflowcheck" {
			t.Errorf("finding %+v: wrong rule/category", f)
		}
	}
}

// writeTemporalModule lays out a fixture module requiring go.temporal.io/sdk
// with one workflow file. The in-process scan is parse-only, so the SDK never
// needs to resolve.
func writeTemporalModule(t *testing.T, workflowSrc string) string {
	t.Helper()
	dir := t.TempDir()
	writeShapeFile(t, dir, "go.mod", "module example.com/wf\n\ngo 1.26\n\nrequire go.temporal.io/sdk v1.30.0\n")
	writeShapeFile(t, dir, "wf/workflow.go", workflowSrc)
	return dir
}
