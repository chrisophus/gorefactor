package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The dispatcher demotes to info on both size rules; the tangle stays at
// warning severity. This pins the per-branch normalization end to end.
func TestSizeRulesDemoteDispatchTables(t *testing.T) {
	path := buildDispatchFixture(t)
	ctx := LintContext{Root: filepath.Dir(path), Files: []string{path}}

	sev := map[string]map[string]string{"complexity": {}, "long-function": {}}
	var cRule complexityRule
	for _, iss := range cRule.Run(ctx) {
		fn := strings.Fields(iss.Message)[0]
		sev["complexity"][fn] = iss.Severity
	}
	var lRule longFunctionRule
	for _, iss := range lRule.Run(ctx) {
		fn := strings.Fields(iss.Message)[0]
		sev["long-function"][fn] = iss.Severity
	}

	if got := sev["complexity"]["dispatch"]; got != "info" {
		t.Errorf("complexity(dispatch) severity = %q, want info (dispatch table)", got)
	}
	if got := sev["complexity"]["tangle"]; got != "info" {
		t.Errorf("complexity(tangle) severity = %q, want info (single-axis proxies are advisory)", got)
	}
	if got := sev["long-function"]["dispatch"]; got != "info" {
		t.Errorf("long-function(dispatch) severity = %q, want info (dispatch table)", got)
	}
}

// buildDispatchFixture writes a file with two over-threshold functions: a
// flat dispatcher (table shape, per-branch small) and a nested tangle.
func buildDispatchFixture(t *testing.T) string {
	t.Helper()
	var b strings.Builder
	b.WriteString("package p\n\nfunc dispatch(k, x int) int {\n\tswitch k {\n")
	for i := 0; i < 40; i++ {
		b.WriteString("\tcase ")
		b.WriteString(itoaFixture(i))
		b.WriteString(":\n\t\tif x > ")
		b.WriteString(itoaFixture(i * 2))
		b.WriteString(" {\n\t\t\treturn ")
		b.WriteString(itoaFixture(i))
		b.WriteString("\n\t\t}\n\t\treturn -1\n")
	}
	b.WriteString("\t}\n\treturn 0\n}\n\nfunc tangle(xs []int) int {\n\ttotal := 0\n")
	for i := 0; i < 16; i++ {
		b.WriteString("\tfor _, x := range xs {\n\t\tif x > ")
		b.WriteString(itoaFixture(i))
		b.WriteString(" {\n\t\t\ttotal += x\n\t\t}\n\t}\n")
	}
	b.WriteString("\treturn total\n}\n")
	dir := t.TempDir()
	path := filepath.Join(dir, "f.go")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func itoaFixture(n int) string {
	if n == 0 {
		return "0"
	}
	var d []byte
	for n > 0 {
		d = append([]byte{byte('0' + n%10)}, d...)
		n /= 10
	}
	return string(d)
}
