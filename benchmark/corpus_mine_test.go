package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seedCorpus writes the given entries as JSONL under dir's failure-corpus
// path, returning dir. Hermetic: no network, temp dir only.
func seedCorpus(t *testing.T, entries []minedFailure) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, failureCorpusRelPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	for _, e := range entries {
		raw, _ := json.Marshal(e)
		b.Write(raw)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestNormalizeReasonClustersVariants(t *testing.T) {
	// Same defect, different symbol/file/line → identical normalized key.
	a := normalizeReason(`ERROR: symbol "Foo" not found in bar/baz.go:42`)
	b := normalizeReason(`ERROR: symbol "Qux" not found in other/thing.go:7`)
	if a != b {
		t.Errorf("expected identical keys, got %q vs %q", a, b)
	}
}

func TestClusterFailuresCountsAndOrders(t *testing.T) {
	entries := []minedFailure{
		{Kind: failRejectedOp, Tool: "replace_code", Reason: `symbol "A" not found`, Spec: "s1"},
		{Kind: failRejectedOp, Tool: "replace_code", Reason: `symbol "B" not found`, Spec: "s2"},
		{Kind: failRejectedOp, Tool: "replace_code", Reason: `symbol "C" not found`, Spec: "s1"}, // dup spec
		{Kind: failCapabilityGap, Tool: "widget", Reason: "no such command"},
	}
	clusters := clusterFailures(entries)
	if len(clusters) != 2 {
		t.Fatalf("expected 2 clusters, got %d", len(clusters))
	}
	// Highest count first.
	if clusters[0].Count != 3 || clusters[0].Tool != "replace_code" {
		t.Errorf("expected replace_code x3 first, got %+v", clusters[0])
	}
	// Distinct specs deduped (s1,s2 — not s1 twice).
	if len(clusters[0].Specs) != 2 {
		t.Errorf("expected 2 distinct specs, got %v", clusters[0].Specs)
	}
}

func TestDiagnoseClusterMapping(t *testing.T) {
	cases := []struct {
		kind  string
		class string
		exp   expectedOutcome
	}{
		{failCapabilityGap, "missing-capability", outEfficient},
		{failRejectedOp, "routing-or-guardrail", outEfficient},
		{failBudgetHit, "efficiency-regression", outEfficient},
		{failPunt, "punt-judgment-gap", outFriction},
	}
	for _, c := range cases {
		class, exp := diagnoseCluster(failureCluster{Kind: c.kind})
		if class != c.class || exp != c.exp {
			t.Errorf("kind %s: got (%s,%s) want (%s,%s)", c.kind, class, exp, c.class, c.exp)
		}
	}
}

func TestRunMineFailuresThresholdAndEmit(t *testing.T) {
	dir := seedCorpus(t, []minedFailure{
		{Kind: failCapabilityGap, Tool: "widget", Reason: "no such command", Spec: "add a widget"},
		{Kind: failCapabilityGap, Tool: "widget", Reason: "no such command", Spec: "make a widget"},
		{Kind: failRejectedOp, Tool: "replace_code", Reason: `symbol "X" not found`, Spec: "rename X"}, // singleton
	})

	var out bytes.Buffer
	n, err := runMineFailures(dir, 2, true, &out)
	if err != nil {
		t.Fatal(err)
	}
	// Only the widget cluster (count 2) graduates; the singleton is noise.
	if n != 1 {
		t.Fatalf("expected 1 graduating cluster, got %d", n)
	}
	if !strings.Contains(out.String(), "missing-capability") {
		t.Errorf("expected diagnosis in output, got:\n%s", out.String())
	}

	// The stub file exists and carries a compilable-looking agentTask literal.
	stub, err := os.ReadFile(filepath.Join(dir, ".gorefactor", "mined_tasks.go.txt"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(stub)
	for _, want := range []string{"ID: \"mined-widget-1\"", "Expected: outEfficient", "Fixture: map[string]string{", `"go.mod": gomod`} {
		if !strings.Contains(s, want) {
			t.Errorf("stub missing %q; got:\n%s", want, s)
		}
	}
}

func TestRunMineFailuresMissingCorpus(t *testing.T) {
	var out bytes.Buffer
	n, err := runMineFailures(t.TempDir(), 2, false, &out)
	if err != nil {
		t.Fatalf("missing corpus should not error, got %v", err)
	}
	if n != 0 || !strings.Contains(out.String(), "nothing to mine") {
		t.Errorf("expected graceful no-op, got n=%d out=%q", n, out.String())
	}
}
