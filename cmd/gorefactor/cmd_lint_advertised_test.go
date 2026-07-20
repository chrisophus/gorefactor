package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestAdvertisedButUnwired_FlagsPhantomOpType is the P3 acceptance for the
// advertised-but-unwired sensor: an example plan JSON that names an operation
// type no executor dispatches must produce a finding.
func TestAdvertisedButUnwired_FlagsPhantomOpType(t *testing.T) {
	dir := writeLivenessFixture(t, map[string]string{
		"examples/phantom_plan.json": `{
  "version": "1.0",
  "name": "phantom",
  "operations": [
    {"type": "not_a_real_op", "file": "x.go"}
  ]
}`,
	})
	issues := (advertisedButUnwiredRule{}).Run(LintContext{Root: dir})
	if len(issues) != 1 {
		t.Fatalf("got %d findings, want 1: %+v", len(issues), issues)
	}
	if issues[0].Rule != "advertised-but-unwired" {
		t.Errorf("rule=%q, want advertised-but-unwired", issues[0].Rule)
	}
	if !strings.Contains(issues[0].Message, "not_a_real_op") {
		t.Errorf("finding does not name the phantom type: %s", issues[0].Message)
	}
	if !strings.Contains(issues[0].File, "phantom_plan.json") {
		t.Errorf("finding file=%q, want phantom_plan.json", issues[0].File)
	}
}

// TestAdvertisedButUnwired_WiredOpTypeIsSilent pins the negative: a plan that
// only names known operation types must not fire.
func TestAdvertisedButUnwired_WiredOpTypeIsSilent(t *testing.T) {
	dir := writeLivenessFixture(t, map[string]string{
		"examples/live_plan.json": `{
  "version": "1.0",
  "name": "live",
  "operations": [
    {"type": "rename_declaration", "file": "x.go", "target": {"functionName": "foo"}, "parameters": {"newName": "bar"}}
  ]
}`,
	})
	if issues := (advertisedButUnwiredRule{}).Run(LintContext{Root: dir}); len(issues) != 0 {
		t.Fatalf("wired plan produced findings: %+v", issues)
	}
}

// TestAdvertisedButUnwired_NonPlanJSONSkipped ensures ordinary JSON (no
// operations) is not treated as a broken advertisement.
func TestAdvertisedButUnwired_NonPlanJSONSkipped(t *testing.T) {
	dir := writeLivenessFixture(t, map[string]string{
		"testdata/ignored.json": `{"type": "not_a_real_op"}`,
		"config.json":           `{"hello": "world"}`,
	})
	issues := (advertisedButUnwiredRule{}).Run(LintContext{Root: dir})
	for _, iss := range issues {
		if strings.Contains(iss.File, "testdata") {
			t.Errorf("testdata must be skipped, got %s", iss.File)
		}
		if filepath.Base(iss.File) == "config.json" {
			t.Errorf("non-plan JSON must be skipped, got %s", iss.File)
		}
	}
}
