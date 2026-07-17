package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const hoistFixture = `package p

import (
	"regexp"
)

var alreadyHoisted = regexp.MustCompile("^ok$")

func parseHunk(s string) bool {
	re := regexp.MustCompile("^@@ -(\\d+)")
	return re.MatchString(s)
}

func dynamic(pat, s string) bool {
	re := regexp.MustCompile(pat)
	return re.MatchString(s)
}

func fallible(s string) error {
	_, err := regexp.Compile("^x$")
	return err
}
`

func TestRegexpHoistRuleFindings(t *testing.T) {
	path := writeHoistFixture(t)
	issues := regexpHoistRule{}.Run(LintContext{Root: filepath.Dir(path), Files: []string{path}})
	if len(issues) != 2 {
		t.Fatalf("want MustCompile-literal + Compile-literal findings, got %+v", issues)
	}
	byFix := map[bool]int{}
	for _, iss := range issues {
		byFix[iss.AutoFixCmd != ""]++
	}
	if byFix[true] != 1 || byFix[false] != 1 {
		t.Fatalf("only the MustCompile site gets the autofix: %+v", issues)
	}
}

func TestHoistRegexpAutofix(t *testing.T) {
	path := writeHoistFixture(t)
	n, err := hoistRegexpInFile(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("want 1 hoist (literal MustCompile only), got %d", n)
	}
	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	src := string(out)
	if !strings.Contains(src, "var parseHunkRe = regexp.MustCompile") {
		t.Fatalf("package-level var missing:\n%s", src)
	}
	if !strings.Contains(src, "re := parseHunkRe") {
		t.Fatalf("call site not rewritten to the var:\n%s", src)
	}
	if !strings.Contains(src, "regexp.MustCompile(pat)") {
		t.Fatalf("dynamic pattern must be untouched:\n%s", src)
	}
	// The rule must be clean after its own fix (sensor/autofix agreement).
	issues := regexpHoistRule{}.Run(LintContext{Root: filepath.Dir(path), Files: []string{path}})
	for _, iss := range issues {
		if iss.AutoFixCmd != "" {
			t.Fatalf("fixable finding survived the fix: %+v", iss)
		}
	}
}

func TestHoistRegexpNameCollision(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.go")
	src := "package p\n\nimport \"regexp\"\n\nvar fooRe = 1\n\nfunc foo() { _ = regexp.MustCompile(\"a\") }\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := hoistRegexpInFile(path, ""); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(path)
	if !strings.Contains(string(out), "var fooRe2 = regexp.MustCompile") {
		t.Fatalf("collision should produce fooRe2:\n%s", out)
	}
}

func writeHoistFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "hoist.go")
	if err := os.WriteFile(path, []byte(hoistFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
