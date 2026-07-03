package main

import (
	"strings"
	"testing"
)

// TestChangeSignatureValidation covers the flat-schema -> CLI-flag mapping's
// guard rails. These branches return before shelling out, so they need no
// gorefactor binary and pin the schema-ambiguity risk flagged in the plan.
func TestChangeSignatureValidation(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
		want string // substring the result must contain
	}{
		{"missing file", map[string]any{"symbol": "Foo", "mode": "add_param"}, "'file' and 'symbol' are required"},
		{"missing symbol", map[string]any{"file": "x.go", "mode": "add_param"}, "'file' and 'symbol' are required"},
		{"bad mode", map[string]any{"file": "x.go", "symbol": "Foo", "mode": "reorder"}, "must be one of add_param"},
		{"add without spec", map[string]any{"file": "x.go", "symbol": "Foo", "mode": "add_param"}, `needs param_spec`},
		{"add with one-word spec", map[string]any{"file": "x.go", "symbol": "Foo", "mode": "add_param", "param_spec": "ctx"}, `needs param_spec`},
		{"remove without param", map[string]any{"file": "x.go", "symbol": "Foo", "mode": "remove_param"}, "needs 'param'"},
		{"rename without names", map[string]any{"file": "x.go", "symbol": "Foo", "mode": "rename_param", "old_name": "a"}, "needs 'old_name' and 'new_name'"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := applyChangeSignature(tc.args)
			if !strings.Contains(got, tc.want) {
				t.Fatalf("applyChangeSignature(%v) = %q, want substring %q", tc.args, got, tc.want)
			}
		})
	}
}
