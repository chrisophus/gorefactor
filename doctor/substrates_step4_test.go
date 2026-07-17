package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseDeadcodeJSON(t *testing.T) {
	// Field names match cmd/deadcode's untagged structs (capitalized keys);
	// unmarshal matching is case-insensitive so lowercase also works.
	out := []byte(`[
	  {"Name": "analyzer", "Path": "example.com/m/analyzer", "Funcs": [
	    {"Name": "Unused", "Position": {"File": "analyzer/a.go", "Line": 10, "Col": 1}},
	    {"Name": "GenUnused", "Position": {"File": "analyzer/gen.go", "Line": 5, "Col": 1}, "Generated": true}
	  ]}
	]`)
	findings, err := parseDeadcodeJSON(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1 (generated dropped)", len(findings))
	}
	f := findings[0]
	if f.Rule != "deadcode/unreachable-func" || f.Category != CategoryDead {
		t.Errorf("rule/category = %s/%s", f.Rule, f.Category)
	}
	if f.File != "analyzer/a.go" || f.Line != 10 {
		t.Errorf("position = %s:%d", f.File, f.Line)
	}
	if !strings.Contains(f.Message, "example.com/m/analyzer.Unused") {
		t.Errorf("message = %q", f.Message)
	}
}

func TestParseDeadcodeJSON_Malformed(t *testing.T) {
	if _, err := parseDeadcodeJSON([]byte("not json")); err == nil {
		t.Fatal("want parse error")
	}
}

func TestParseGovulncheckJSON(t *testing.T) {
	out := []byte(`
{"config": {"protocol_version": "v1.0.0"}}
{"progress": {"message": "Scanning..."}}
{"osv": {"id": "GO-2024-1234", "summary": "Bad parsing in example.com/vuln"}}
{"finding": {"osv": "GO-2024-1234", "fixed_version": "v1.2.3", "trace": [{"module": "example.com/vuln"}]}}
{"finding": {"osv": "GO-2024-1234", "fixed_version": "v1.2.3", "trace": [{"module": "example.com/vuln", "package": "example.com/vuln/pkg"}]}}
{"finding": {"osv": "GO-2024-1234", "fixed_version": "v1.2.3", "trace": [
  {"module": "example.com/vuln", "package": "example.com/vuln/pkg", "function": "Parse"},
  {"module": "example.com/m", "package": "example.com/m/a", "function": "Use", "position": {"filename": "a/use.go", "line": 42}}
]}}
`)
	findings, err := parseGovulncheckJSON(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1 (only symbol-level reports)", len(findings))
	}
	f := findings[0]
	if f.Rule != "govulncheck/GO-2024-1234" || f.Category != CategorySec {
		t.Errorf("rule/category = %s/%s", f.Rule, f.Category)
	}
	if f.File != "a/use.go" || f.Line != 42 {
		t.Errorf("call site = %s:%d", f.File, f.Line)
	}
	for _, want := range []string{"example.com/vuln/pkg.Parse is reachable", "Bad parsing", "fixed in v1.2.3"} {
		if !strings.Contains(f.Message, want) {
			t.Errorf("message %q missing %q", f.Message, want)
		}
	}
}

func TestParseGovulncheckJSON_DedupsRepeatFindings(t *testing.T) {
	out := []byte(`
{"finding": {"osv": "GO-2024-1", "trace": [{"package": "p", "function": "F"}]}}
{"finding": {"osv": "GO-2024-1", "trace": [{"package": "p", "function": "F"}]}}
`)
	findings, err := parseGovulncheckJSON(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1 after dedup", len(findings))
	}
}

func TestParseTidyDiff(t *testing.T) {
	out := []byte(`diff current/go.mod tidy/go.mod
--- current/go.mod
+++ tidy/go.mod
@@ -5,7 +5,6 @@
 require (
-	github.com/unused/dep v1.0.0
+	github.com/missing/dep v2.1.0
 )
diff current/go.sum tidy/go.sum
--- current/go.sum
+++ tidy/go.sum
@@ -1,2 +1,1 @@
-github.com/unused/dep v1.0.0 h1:abc=
`)
	findings := parseTidyDiff(out)
	if len(findings) != 2 {
		t.Fatalf("findings = %d, want 2: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `unneeded requirement "github.com/unused/dep v1.0.0"`) {
		t.Errorf("finding 0 = %q", findings[0].Message)
	}
	if !strings.Contains(findings[1].Message, `missing requirement "github.com/missing/dep v2.1.0"`) {
		t.Errorf("finding 1 = %q", findings[1].Message)
	}
	for _, f := range findings {
		if f.FixCmd != "go mod tidy" || f.Category != CategoryDead {
			t.Errorf("finding %+v: want go mod tidy fix, dead category", f)
		}
	}
}

func TestParseTidyDiff_SumOnly(t *testing.T) {
	out := []byte(`diff current/go.sum tidy/go.sum
--- current/go.sum
+++ tidy/go.sum
@@ -1,2 +1,1 @@
-github.com/x/y v1.0.0 h1:abc=
`)
	findings := parseTidyDiff(out)
	if len(findings) != 1 || findings[0].File != "go.sum" {
		t.Fatalf("findings = %+v, want single go.sum finding", findings)
	}
}

func TestParseTidyDiff_Tidy(t *testing.T) {
	if findings := parseTidyDiff(nil); len(findings) != 0 {
		t.Fatalf("findings = %+v, want none", findings)
	}
}

func TestDetectShape(t *testing.T) {
	dir := t.TempDir()
	writeShapeFile(t, dir, "go.mod", `module example.com/m

go 1.26

require (
	go.temporal.io/sdk v1.30.0
	github.com/segmentio/kafka-go v0.4.47
)
`)
	writeShapeFile(t, dir, "lib/lib.go", "package lib\n")
	writeShapeFile(t, dir, "cmd/tool/main.go", "package main\n")
	shape, err := DetectShape(dir)
	if err != nil {
		t.Fatal(err)
	}
	if shape.ModulePath != "example.com/m" || shape.GoVersion != "1.26" {
		t.Errorf("module/go = %s/%s", shape.ModulePath, shape.GoVersion)
	}
	if !shape.HasTemporal || !shape.HasKafka {
		t.Errorf("temporal/kafka = %v/%v, want true/true", shape.HasTemporal, shape.HasKafka)
	}
	if shape.IsLibrary {
		t.Error("IsLibrary = true, want false (cmd/tool is a main package)")
	}
	if len(shape.MainDirs) != 1 || shape.MainDirs[0] != "cmd/tool" {
		t.Errorf("MainDirs = %v", shape.MainDirs)
	}
}

func TestDetectShape_Library(t *testing.T) {
	dir := t.TempDir()
	writeShapeFile(t, dir, "go.mod", "module example.com/lib\n\ngo 1.26\n")
	writeShapeFile(t, dir, "lib.go", "package lib\n")
	shape, err := DetectShape(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !shape.IsLibrary || shape.HasTemporal || shape.HasKafka {
		t.Errorf("shape = %+v, want pure library", shape)
	}
}

func writeShapeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
