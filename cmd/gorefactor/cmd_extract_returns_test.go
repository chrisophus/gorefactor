package main

import (
	"os"
	"strings"
	"testing"
)

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
