package doctor

import "testing"

func TestParseGolangciJSON(t *testing.T) {
	out := []byte(`{"Issues":[
		{"FromLinter":"gosec","Text":"weak rand","Pos":{"Filename":"a.go","Line":7}},
		{"FromLinter":"staticcheck","Text":"unreachable","Pos":{"Filename":"b.go","Line":2}}
	]}`)
	findings, err := parseGolangciJSON(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 2 {
		t.Fatalf("want 2 findings: %+v", findings)
	}
	if findings[0].Rule != "golangci/gosec" || findings[0].Category != CategorySec || findings[0].Line != 7 {
		t.Fatalf("gosec mapping wrong: %+v", findings[0])
	}
	if findings[1].Category != CategoryLint {
		t.Fatalf("unmapped linters land in the lint category: %+v", findings[1])
	}
}

func TestGolangciCategoryMapping(t *testing.T) {
	for linter, want := range map[string]Category{
		"gosec":        CategorySec,
		"unused":       CategoryDead,
		"prealloc":     CategoryPerf,
		"contextcheck": CategoryConc,
		"containedctx": CategoryConc,
		"gocritic":     CategoryLint,
	} {
		if got := golangciCategory(linter); got != want {
			t.Errorf("golangciCategory(%q) = %s, want %s", linter, got, want)
		}
	}
}

func TestPreflightReportsProbeFailures(t *testing.T) {
	ok := &fakeSubstrate{info: SubstrateInfo{Name: "fine"}}
	bad := &fakeSubstrate{info: SubstrateInfo{Name: "broken"}, probeErr: unavailablef("no binary")}
	failures := Preflight(".", []Substrate{ok, bad})
	if len(failures) != 1 {
		t.Fatalf("want 1 failure: %v", failures)
	}
	if _, found := failures["broken"]; !found {
		t.Fatalf("broken substrate should be reported: %v", failures)
	}
}

// TestParseGolangciJSON_TrailingStats covers golangci v2 appending a text
// stats line after the JSON object on stdout: only the first JSON value is
// decoded.
func TestParseGolangciJSON_TrailingStats(t *testing.T) {
	out := []byte(`{"Issues": [{"FromLinter": "errcheck", "Text": "unchecked", "Pos": {"Filename": "a.go", "Line": 3}}]}
2 issues:
* errcheck: 2
`)
	findings, err := parseGolangciJSON(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].Rule != "golangci/errcheck" {
		t.Fatalf("findings = %+v", findings)
	}
}
