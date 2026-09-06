package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSplitSarifLocation(t *testing.T) {
	cases := []struct {
		in        string
		path      string
		line, col int
	}{
		{"internal/gitx/git.go", "internal/gitx/git.go", 0, 0},
		{"internal/report/serve.go:187", "internal/report/serve.go", 187, 0},
		{"cmd/redline/main_test.go:25:1", "cmd/redline/main_test.go", 25, 1},
		{"internal/pane", "internal/pane", 0, 0},
	}
	for _, c := range cases {
		p, l, col := splitSarifLocation(c.in)
		if p != c.path || l != c.line || col != c.col {
			t.Errorf("splitSarifLocation(%q) = (%q,%d,%d), want (%q,%d,%d)",
				c.in, p, l, col, c.path, c.line, c.col)
		}
	}
}

func TestSarifLevel(t *testing.T) {
	for sev, want := range map[string]string{
		"error":   "error",
		"warning": "warning",
		"info":    "note",
		"":        "note",
	} {
		if got := sarifLevel(sev); got != want {
			t.Errorf("sarifLevel(%q) = %q, want %q", sev, got, want)
		}
	}
}

func TestBuildSarifLog(t *testing.T) {
	issues := []lintIssue{
		{File: "internal/gitx/git.go", Rule: "file-size", Severity: "error", Message: "file too long"},
		{File: "cmd/x/main_test.go:25:1", Rule: "funcorder-function", Severity: "warning", Message: "order"},
		{File: "internal/gitx/git.go", Rule: "file-size", Severity: "error", Message: "again"},
	}
	log := buildSarifLog(issues)

	if log.Version != sarifVersion || log.Schema == "" {
		t.Fatalf("bad envelope: version=%q schema=%q", log.Version, log.Schema)
	}
	if len(log.Runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(log.Runs))
	}
	run := log.Runs[0]
	if run.Tool.Driver.Name != "gorefactor" || run.Tool.Driver.Version == "" {
		t.Errorf("driver = %+v", run.Tool.Driver)
	}
	// Rules deduped, first-appearance order.
	if len(run.Tool.Driver.Rules) != 2 ||
		run.Tool.Driver.Rules[0].ID != "file-size" ||
		run.Tool.Driver.Rules[1].ID != "funcorder-function" {
		t.Errorf("driver rules = %+v", run.Tool.Driver.Rules)
	}
	if len(run.Results) != 3 {
		t.Fatalf("results = %d, want 3", len(run.Results))
	}
	// Whole-file finding: no region, level maps error -> error.
	if run.Results[0].Locations[0].PhysicalLocation.Region != nil {
		t.Error("whole-file finding must have no region")
	}
	if run.Results[0].Level != "error" {
		t.Errorf("level = %q, want error", run.Results[0].Level)
	}
	// file:line:col finding: region carries line and column, uri is stripped.
	loc := run.Results[1].Locations[0].PhysicalLocation
	if loc.ArtifactLocation.URI != "cmd/x/main_test.go" {
		t.Errorf("uri = %q, want cmd/x/main_test.go", loc.ArtifactLocation.URI)
	}
	if loc.Region == nil || loc.Region.StartLine != 25 || loc.Region.StartColumn != 1 {
		t.Errorf("region = %+v, want line 25 col 1", loc.Region)
	}
}

// TestBuildSarifLog_MarshalsWithoutEmptyRegion pins the wire shape: a
// whole-file finding must not emit a region object (its startLine would be an
// invalid 0), and the schema/driver keys must be present.
func TestBuildSarifLog_MarshalsWithoutEmptyRegion(t *testing.T) {
	log := buildSarifLog([]lintIssue{
		{File: "a.go", Rule: "file-size", Severity: "error", Message: "x"},
	})
	b, err := json.Marshal(log)
	if err != nil {
		t.Fatal(err)
	}
	out := string(b)
	if strings.Contains(out, "\"region\"") {
		t.Errorf("whole-file finding emitted a region: %s", out)
	}
	for _, want := range []string{"\"$schema\"", "\"driver\"", "\"ruleId\":\"file-size\"", "\"level\":\"error\""} {
		if !strings.Contains(out, want) {
			t.Errorf("SARIF output missing %s: %s", want, out)
		}
	}
}
