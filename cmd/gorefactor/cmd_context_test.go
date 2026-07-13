package main

import (
	"encoding/json"
	"strings"
	"testing"
)

const contextTestMain = `package x

// Order is a purchase order.
type Order struct {
	ID    string
	Total int
}

// Validate checks an order for sanity.
func Validate(o Order) error {
	if o.Total < 0 {
		return nil
	}
	return nil
}

func process(o Order) {
	_ = Validate(o)
}
`

const contextTestTests = `package x

import "testing"

func TestValidate(t *testing.T) {
	_ = Validate(Order{})
}
`

func TestContextPackSections(t *testing.T) {
	t.Chdir(t.TempDir())
	writeContextFixture(t)

	out := captureStdout(t, func() {
		if err := contextCommand([]string{"Validate"}); err != nil {
			t.Errorf("context: %v", err)
		}
	})
	for _, want := range []string{
		"── Validate (order.go:10) ──",
		"// Validate checks an order for sanity.",
		"func Validate(o Order) error {",
		"── callers (1) ──",
		"func process(o Order)",
		"> 18: \t_ = Validate(o)",
		"── signature types ──",
		"type Order struct {",
		"── tests ──",
		"TestValidate",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("context output missing %q:\n%s", want, out)
		}
	}
}

func TestContextJSONShape(t *testing.T) {
	t.Chdir(t.TempDir())
	writeContextFixture(t)

	out := captureStdout(t, func() {
		if err := contextCommand([]string{"Validate", "--json"}); err != nil {
			t.Errorf("context --json: %v", err)
		}
	})
	var pack contextPack
	if err := json.Unmarshal([]byte(out), &pack); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if pack.Symbol != "Validate" || pack.File != "order.go" || pack.Line != 10 {
		t.Fatalf("unexpected header: %+v", pack)
	}
	if pack.Doc != "Validate checks an order for sanity." {
		t.Fatalf("doc = %q", pack.Doc)
	}
	if len(pack.Callers) != 1 || pack.Callers[0].Name != "process" {
		t.Fatalf("callers = %+v", pack.Callers)
	}
	if len(pack.Types) != 1 || pack.Types[0].Name != "Order" {
		t.Fatalf("types = %+v", pack.Types)
	}
	if len(pack.Tests) != 1 || pack.Tests[0] != "TestValidate" {
		t.Fatalf("tests = %+v", pack.Tests)
	}
}

func TestContextBudgetTruncates(t *testing.T) {
	t.Chdir(t.TempDir())
	writeContextFixture(t)

	out := captureStdout(t, func() {
		if err := contextCommand([]string{"Validate", "--budget", "200", "--json"}); err != nil {
			t.Errorf("context --budget: %v", err)
		}
	})
	var pack contextPack
	if err := json.Unmarshal([]byte(out), &pack); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if !pack.Truncated {
		t.Fatalf("budget 200 should force truncation: %+v", pack)
	}
	if len(pack.Notes) == 0 {
		t.Fatal("truncation must leave a note")
	}
	if len(renderContextPack(&pack)) > 200+100 { // small slack for the trailing notes line
		t.Fatalf("rendered pack still exceeds budget: %d chars", len(renderContextPack(&pack)))
	}
	// the definition is the highest-value section and must survive
	if pack.Definition == "" {
		t.Fatal("definition must never be dropped entirely")
	}
}

func TestContextTypeSymbol(t *testing.T) {
	t.Chdir(t.TempDir())
	writeContextFixture(t)

	out := captureStdout(t, func() {
		if err := contextCommand([]string{"Order"}); err != nil {
			t.Errorf("context on type: %v", err)
		}
	})
	if !strings.Contains(out, "// Order is a purchase order.") || !strings.Contains(out, "type Order struct {") {
		t.Fatalf("type context missing definition:\n%s", out)
	}
}

func TestContextNotFound(t *testing.T) {
	t.Chdir(t.TempDir())
	writeContextFixture(t)
	err := contextCommand([]string{"Missing"})
	assertExitCode(t, err, exitNotFound)
}

func writeContextFixture(t *testing.T) {
	t.Helper()
	writeTempGo(t, ".", "order.go", contextTestMain)
	writeTempGo(t, ".", "order_test.go", contextTestTests)
}
