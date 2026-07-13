package main

import (
	"strings"
	"testing"
)

// TestErrWrapRuleAutoFixDoesNotShellOut proves errWrapRule.AutoFix applies
// the fix in-process rather than depending on a "gorefactor" binary being on
// $PATH (it previously shelled out via exec.Command("gorefactor", ...), which
// silently failed whenever only a local ./gorefactor binary existed, as in
// this test environment).
func TestErrWrapRuleAutoFixDoesNotShellOut(t *testing.T) {
	dir := t.TempDir()
	path := writeTempGo(t, dir, "f.go",
		"package x\n\nimport \"os\"\n\nfunc doWork() error {\n\tf, err := os.Open(\"nope.txt\")\n\tif err != nil {\n\t\treturn err\n\t}\n\tf.Close()\n\treturn nil\n}\n")

	iss := lintIssue{
		File:       path,
		Rule:       "error-not-wrapped",
		AutoFixCmd: "wrap-errors " + path + " doWork",
	}

	if err := (errWrapRule{}).AutoFix(iss, LintContext{}); err != nil {
		t.Fatalf("AutoFix: %v", err)
	}
	got := readFile(t, path)
	if !strings.Contains(got, "fmt.Errorf(") || strings.Contains(got, "return err\n") {
		t.Fatalf("expected bare return err to be wrapped; got:\n%s", got)
	}
}
