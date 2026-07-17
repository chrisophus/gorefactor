package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// mustBuildModule compiles the temp module containing path, failing the test on
// any compile error — the extraction regressions produced build errors, so
// parse-validity alone would not catch them.
func mustBuildModule(t *testing.T, path string) {
	t.Helper()
	cmd := exec.Command("go", "build", "./...")
	cmd.Dir = filepath.Dir(path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("module did not build after extraction: %v\n%s", err, out)
	}
}

// Without --allow-returns a return-bearing block is refused, exactly as before.
func TestExtract_ReturnsRefusedWithoutFlag(t *testing.T) {
	path := writeComplexityModule(t, liftableSrc)
	err := extractCommand([]string{path, "6", "11", "validateItems"})
	if err == nil {
		t.Fatal("expected refusal for return-bearing block without --allow-returns")
	}
	if !strings.Contains(err.Error(), "return statement") {
		t.Fatalf("expected return-statement refusal, got: %v", err)
	}
}

const liftableSrc = `package cxmod

import "errors"

func Process(items []string) (int, error) {
	if len(items) == 0 {
		return 0, errors.New("no items")
	}
	if len(items) > 100 {
		return 0, errors.New("too many items")
	}
	count := 0
	for _, it := range items {
		count += len(it)
	}
	return count, nil
}
`

// With --allow-returns the returns are lifted into a (results..., done bool)
// helper and the call site propagates a taken return.
func TestExtract_AllowReturnsLiftsBlock(t *testing.T) {
	path := writeComplexityModule(t, liftableSrc)
	if err := extractCommand([]string{path, "6", "11", "validateItems", "--allow-returns"}); err != nil {
		t.Fatalf("extract --allow-returns: %v", err)
	}
	mustParse(t, path)
	got := readBack(t, path)
	for _, want := range []string{
		"func validateItems(items []string) (r0 int, r1 error, done bool)",
		"return 0, errors.New(\"no items\"), true",
		"if r0, r1, done := validateItems(items); done {",
		"return r0, r1",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rewritten file missing %q\n---\n%s", want, got)
		}
	}
}

// A block that both returns and assigns an outer variable used later cannot be
// lifted mechanically; it must be refused, not silently miscompiled.
func TestExtract_AllowReturnsRefusesMixedWriteAndReturn(t *testing.T) {
	const src = `package cxmod

import "errors"

func Sum(items []string) (int, error) {
	total := 0
	for _, it := range items {
		if it == "" {
			return 0, errors.New("empty")
		}
		total += len(it)
	}
	return total, nil
}
`
	path := writeComplexityModule(t, src)
	before := readBack(t, path)
	err := extractCommand([]string{path, "7", "12", "sumItems", "--allow-returns"})
	if err == nil {
		t.Fatal("expected refusal: block assigns total (used after) and returns")
	}
	if !strings.Contains(err.Error(), "assigns variable(s) used after it") {
		t.Fatalf("expected mixed write+return refusal, got: %v", err)
	}
	if got := readBack(t, path); got != before {
		t.Error("file was mutated despite refusal")
	}
}

// An outer variable assigned inside the extracted block and read afterwards
// must be returned and written back at the call site with = (not :=). This
// was the silent-miscompilation hole in the original engine: the mutation
// died in a by-value parameter.
func TestExtract_WritesBackMutatedOuterVariable(t *testing.T) {
	const src = `package cxmod

func Calc(items []string) int {
	total := 0
	for _, it := range items {
		if it == "x" {
			total += 10
		}
		total += len(it)
	}
	total *= 2
	return total
}
`
	path := writeComplexityModule(t, src)
	if err := extractCommand([]string{path, "5", "10", "sumItems"}); err != nil {
		t.Fatalf("extract: %v", err)
	}
	mustParse(t, path)
	got := readBack(t, path)
	if !strings.Contains(got, "total = sumItems(items, total)") {
		t.Errorf("expected write-back call site `total = sumItems(items, total)`\n---\n%s", got)
	}
	if !strings.Contains(got, "return total") {
		t.Errorf("expected helper to return total\n---\n%s", got)
	}
}

// A return inside a function literal belongs to the literal, not the block:
// it is not a barrier and must not be rewritten.
func TestExtract_ClosureReturnIsNotABarrier(t *testing.T) {
	const src = `package cxmod

import "sort"

func Order(xs []int) {
	sort.Slice(xs, func(i, j int) bool {
		return xs[i] < xs[j]
	})
	sort.Slice(xs, func(i, j int) bool {
		return xs[i] > xs[j]
	})
}
`
	path := writeComplexityModule(t, src)
	if err := extractCommand([]string{path, "6", "8", "sortAsc"}); err != nil {
		t.Fatalf("closure-only returns should extract without --allow-returns: %v", err)
	}
	mustParse(t, path)
	got := readBack(t, path)
	if strings.Contains(got, "done bool") {
		t.Errorf("closure return was wrongly lifted\n---\n%s", got)
	}
}

// readBack returns the rewritten file content, failing the test on error.
func readBack(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// A return-bearing block that is the function's tail must return the helper's
// values unconditionally — a conditional would leave the function to fall off
// its end (missing return). Regression for the tail-lift fix.
const tailReturnSrc = `package cxmod

func Classify(n int) (string, error) {
	if n < 0 {
		return "neg", nil
	}
	switch {
	case n == 0:
		return "zero", nil
	default:
		return "pos", nil
	}
}
`

func TestExtract_TailReturnLiftUsesUnconditionalReturn(t *testing.T) {
	path := writeComplexityModule(t, tailReturnSrc)
	if err := extractCommand([]string{path, "7", "12", "classifyRest", "--allow-returns"}); err != nil {
		t.Fatalf("extract tail block: %v", err)
	}
	got := readBack(t, path)
	if strings.Contains(got, "if r0, r1, done := classifyRest") {
		t.Errorf("tail block should not use conditional return (falls off end)\n---\n%s", got)
	}
	for _, want := range []string{"r0, r1, _ := classifyRest(n)", "return r0, r1"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q\n---\n%s", want, got)
		}
	}
	mustBuildModule(t, path)
}

// A type-switch case variable (`switch s := x.(type)`) is declared in the block
// and must not be lifted to a parameter. Regression for the info.Implicits fix.
const typeSwitchSrc = `package cxmod

func Describe(n any) string {
	out := ""
	switch s := n.(type) {
	case int:
		out = "int"
		_ = s
	case string:
		out = s
	}
	return out
}
`

func TestExtract_TypeSwitchBindingNotLiftedToParam(t *testing.T) {
	path := writeComplexityModule(t, typeSwitchSrc)
	if err := extractCommand([]string{path, "5", "11", "describeKind"}); err != nil {
		t.Fatalf("extract type-switch block: %v", err)
	}
	got := readBack(t, path)
	if strings.Contains(got, "s any") || strings.Contains(got, ", s ") {
		t.Errorf("type-switch var s wrongly lifted to a parameter\n---\n%s", got)
	}
	mustBuildModule(t, path)
}
