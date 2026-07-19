package main

import (
	"testing"

	"github.com/chrisophus/gorefactor/analyzer"
	"github.com/chrisophus/gorefactor/orchestrator"
)

// TestEmittableOpsAreDispatchable pins the diff analyzer's plan generator to
// the orchestrator's dispatch registry: every operation type the generator
// can emit must be executable, in this package, where the extract/inline
// external-handler bridges are registered. The 2026-07 review found the
// generator emitting rename_variable, which no executor dispatched — every
// generated plan containing one failed. This test makes that class of drift
// (a producer inventing op types the consumer never learned) a build-time
// failure instead of a runtime plan error.
func TestEmittableOpsAreDispatchable(t *testing.T) {
	known := make(map[string]bool)
	for _, op := range orchestrator.KnownOperationTypes() {
		known[op] = true
	}
	for _, op := range analyzer.EmittableOperationTypes() {
		if !known[op] {
			t.Errorf("diff plan generator can emit %q but no executor dispatches it; register a handler or stop emitting it", op)
		}
	}
}

// TestEmittableOpsListIsCurrent guards the mirror itself: the static
// EmittableOperationTypes list must contain the op types changeToOperation
// actually produces for each change kind it maps.
func TestEmittableOpsListIsCurrent(t *testing.T) {
	listed := make(map[string]bool)
	for _, op := range analyzer.EmittableOperationTypes() {
		listed[op] = true
	}
	for _, want := range []string{"insert_code", "extract_method"} {
		if !listed[want] {
			t.Errorf("EmittableOperationTypes is missing %q", want)
		}
	}
}
