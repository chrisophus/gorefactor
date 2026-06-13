package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- trim -------------------------------------------------------------------

func TestTrim_ShortPassthrough(t *testing.T) {
	cases := []struct{ in string }{
		{""},
		{"hello"},
		{"  trimmed  "},
		{strings.Repeat("x", 100)},
	}
	for _, c := range cases {
		got := trim(c.in, 200)
		want := strings.TrimSpace(c.in)
		if got != want {
			t.Errorf("trim(%q, 200) = %q, want %q", c.in, got, want)
		}
	}
}

func TestTrim_HeadTailPreservation(t *testing.T) {
	// Build a long string where the head contains "FIRST_ERROR" and the tail
	// contains "LAST_LINE". The middle should be elided.
	// With max=400: head=266, tail=104 (>60), so head+tail path is taken.
	body := "FIRST_ERROR: something went wrong\n" + strings.Repeat("middle line\n", 60)
	input := body + "LAST_LINE: final state\n"

	max := 400
	got := trim(input, max)

	if len(got) > max+60 { // allow slack for the omission marker itself
		t.Errorf("trim result too long: %d bytes (max %d)", len(got), max)
	}
	if !strings.Contains(got, "FIRST_ERROR") {
		t.Error("trim must preserve the head (first error context)")
	}
	if !strings.Contains(got, "LAST_LINE") {
		t.Error("trim must preserve the tail (final state context)")
	}
	if !strings.Contains(got, "omitted") {
		t.Error("trim should include an omission marker in the middle")
	}
}

func TestTrim_NaiveFallbackWhenTailTooSmall(t *testing.T) {
	// When max is small enough that tail < 60, fall back to naive head-only cut.
	input := strings.Repeat("abcde", 40) // 200 chars
	got := trim(input, 80)
	if !strings.HasSuffix(got, "truncated)") && !strings.Contains(got, "omitted") {
		t.Errorf("expected truncated or omitted marker, got: %q", got)
	}
	// Either way, must not exceed max + overhead
	if len(got) > 200 {
		t.Errorf("trim result too long: %d", len(got))
	}
}

// --- specFromLintIssue ------------------------------------------------------

func TestSpecFromLintIssue_Split(t *testing.T) {
	iss := lintIssueJSON{
		File:       "big.go",
		Rule:       "file-size",
		Message:    "480 lines",
		AutoFixCmd: "gorefactor split big.go",
	}
	got := specFromLintIssue(iss)
	if !strings.Contains(got, "big.go") {
		t.Error("spec should mention the file")
	}
	if !strings.Contains(got, "split_file") {
		t.Error("spec should mention split_file tool")
	}
	if !strings.Contains(got, "sibling") {
		t.Error("spec should mention sibling files")
	}
}

func TestSpecFromLintIssue_WrapErrors(t *testing.T) {
	iss := lintIssueJSON{
		File:       "service.go",
		Rule:       "error-not-wrapped",
		Message:    "bare return err",
		AutoFixCmd: "gorefactor wrap-errors service.go processRequest",
	}
	got := specFromLintIssue(iss)
	if !strings.Contains(got, "processRequest") {
		t.Error("spec should mention the function name")
	}
	if !strings.Contains(got, "service.go") {
		t.Error("spec should mention the file")
	}
	if !strings.Contains(got, "wrap_errors") {
		t.Error("spec should mention wrap_errors tool")
	}
}

func TestSpecFromLintIssue_SetDoc(t *testing.T) {
	iss := lintIssueJSON{
		File:       "api.go",
		Rule:       "missing-godoc",
		Message:    "exported type Handler has no doc comment",
		AutoFixCmd: "gorefactor set-doc api.go Handler -",
	}
	got := specFromLintIssue(iss)
	if !strings.Contains(got, "Handler") {
		t.Error("spec should mention the declaration name")
	}
	if !strings.Contains(got, "api.go") {
		t.Error("spec should mention the file")
	}
	if !strings.Contains(got, "set_doc") {
		t.Error("spec should mention set_doc tool")
	}
}

func TestSpecFromLintIssue_Recommend(t *testing.T) {
	iss := lintIssueJSON{
		File:       "handler.go",
		Rule:       "extract-candidate",
		Message:    "complexity 12 in handleRequest",
		AutoFixCmd: "gorefactor recommend handler.go --function handleRequest",
	}
	got := specFromLintIssue(iss)
	if !strings.Contains(got, "handler.go") {
		t.Error("spec should mention the file")
	}
	if !strings.Contains(got, "extract_method") {
		t.Error("spec should mention extract_method tool")
	}
}

func TestSpecFromLintIssue_AddTest(t *testing.T) {
	iss := lintIssueJSON{
		File:       "util.go",
		Rule:       "untested-function",
		Message:    "Validate has no test",
		AutoFixCmd: "gorefactor add-test util.go Validate",
	}
	got := specFromLintIssue(iss)
	if !strings.Contains(got, "Validate") {
		t.Error("spec should mention the function name")
	}
	if !strings.Contains(got, "_test.go") {
		t.Error("spec should mention the test file")
	}
}

func TestSpecFromLintIssue_Unknown(t *testing.T) {
	iss := lintIssueJSON{
		File:       "foo.go",
		Rule:       "some-new-rule",
		Message:    "something unusual",
		AutoFixCmd: "gorefactor frobnicate foo.go Bar",
	}
	got := specFromLintIssue(iss)
	// Should fall through to the generic format and include rule + message.
	if !strings.Contains(got, "some-new-rule") {
		t.Error("fallback spec should include rule name")
	}
	if !strings.Contains(got, "foo.go") {
		t.Error("fallback spec should include file name")
	}
}

func TestSpecFromLintIssue_EmptyAutofixCmd(t *testing.T) {
	iss := lintIssueJSON{
		File:    "x.go",
		Rule:    "no-fix",
		Message: "something",
		// AutoFixCmd intentionally empty
	}
	got := specFromLintIssue(iss)
	if !strings.Contains(got, "no-fix") || !strings.Contains(got, "x.go") {
		t.Error("empty autofixCmd should produce a generic fallback spec")
	}
}

// --- enumerateFindingsViaLint -----------------------------------------------

// withFakeGorefactor writes a fake gorefactor script to a temp dir and
// prepends that dir to PATH so gorefactorBin() resolves to it.
func withFakeGorefactor(t *testing.T, jsonOut string) string {
	t.Helper()
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "lint_output.json")
	if err := os.WriteFile(jsonPath, []byte(jsonOut), 0o644); err != nil {
		t.Fatal(err)
	}
	// The script cats the pre-baked JSON file regardless of its arguments.
	script := fmt.Sprintf("#!/bin/sh\ncat %s\n", jsonPath)
	binPath := filepath.Join(dir, "gorefactor")
	if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	return dir
}

func TestEnumerateFindingsViaLint_ParsesJSON(t *testing.T) {
	withFakeGorefactor(t, `{
		"issues": [
			{"file":"a.go","rule":"file-size","severity":"warning",
			 "message":"oversized","autofixCmd":"gorefactor split a.go"},
			{"file":"b.go","rule":"error-not-wrapped","severity":"warning",
			 "message":"bare return err","autofixCmd":"gorefactor wrap-errors b.go Foo"}
		],
		"summary":{"total":2,"failing":0}
	}`)

	findings, ok := enumerateFindingsViaLint(t.TempDir())
	if !ok {
		t.Fatal("enumerateFindingsViaLint returned ok=false with valid JSON")
	}
	if len(findings) != 2 {
		t.Fatalf("expected 2 findings, got %d", len(findings))
	}
	if findings[0].kind != "file-size" || findings[0].path != "a.go" {
		t.Errorf("findings[0] = %+v", findings[0])
	}
	if findings[1].kind != "error-not-wrapped" || findings[1].path != "b.go" {
		t.Errorf("findings[1] = %+v", findings[1])
	}
}

func TestEnumerateFindingsViaLint_DeduplicatesByFileRule(t *testing.T) {
	// Same (file, rule) should produce only one finding.
	withFakeGorefactor(t, `{
		"issues": [
			{"file":"a.go","rule":"missing-godoc","severity":"info",
			 "message":"Foo has no doc","autofixCmd":"gorefactor set-doc a.go Foo -"},
			{"file":"a.go","rule":"missing-godoc","severity":"info",
			 "message":"Bar has no doc","autofixCmd":"gorefactor set-doc a.go Bar -"},
			{"file":"b.go","rule":"missing-godoc","severity":"info",
			 "message":"Baz has no doc","autofixCmd":"gorefactor set-doc b.go Baz -"}
		],
		"summary":{"total":3,"failing":0}
	}`)

	findings, ok := enumerateFindingsViaLint(t.TempDir())
	if !ok {
		t.Fatal("ok=false with valid JSON")
	}
	// a.go/missing-godoc should be deduplicated → 2 total
	if len(findings) != 2 {
		t.Fatalf("expected 2 findings after deduplication, got %d: %+v", len(findings), findings)
	}
}

func TestEnumerateFindingsViaLint_SkipsNoAutofixCmd(t *testing.T) {
	withFakeGorefactor(t, `{
		"issues": [
			{"file":"a.go","rule":"complexity","severity":"warning","message":"high complexity","autofixCmd":""},
			{"file":"b.go","rule":"high-coupling","severity":"warning","message":"high coupling"}
		],
		"summary":{"total":2,"failing":0}
	}`)

	findings, ok := enumerateFindingsViaLint(t.TempDir())
	if !ok {
		t.Fatal("ok=false with valid JSON")
	}
	if len(findings) != 0 {
		t.Fatalf("expected 0 fixable findings (no autofixCmd), got %d: %+v", len(findings), findings)
	}
}

func TestEnumerateFindingsViaLint_FallsBackOnBadJSON(t *testing.T) {
	withFakeGorefactor(t, `not valid json at all`)

	_, ok := enumerateFindingsViaLint(t.TempDir())
	if ok {
		t.Fatal("expected ok=false on unparseable JSON output")
	}
}

func TestEnumerateFindingsViaLint_EmptyOutput(t *testing.T) {
	withFakeGorefactor(t, ``)

	_, ok := enumerateFindingsViaLint(t.TempDir())
	// Empty output → false (no findings or binary not available)
	if ok {
		t.Fatal("expected ok=false on empty output")
	}
}

func TestEnumerateFindingsViaLint_SpecContentsMatchRule(t *testing.T) {
	withFakeGorefactor(t, `{
		"issues": [
			{"file":"svc.go","rule":"file-size","severity":"warning",
			 "message":"450 lines","autofixCmd":"gorefactor split svc.go"}
		],
		"summary":{"total":1,"failing":0}
	}`)

	findings, ok := enumerateFindingsViaLint(t.TempDir())
	if !ok || len(findings) != 1 {
		t.Fatalf("expected 1 finding, got ok=%v findings=%d", ok, len(findings))
	}
	f := findings[0]
	if !strings.Contains(f.spec, "svc.go") {
		t.Errorf("spec should mention the file: %q", f.spec)
	}
	if !strings.Contains(f.spec, "split_file") {
		t.Errorf("spec should mention split_file tool: %q", f.spec)
	}
	if f.detail == "" {
		t.Error("finding detail must not be empty")
	}
}

// --- runLintAdvisory --------------------------------------------------------

func TestRunLintAdvisory_ReturnsEmptyWhenNoIssues(t *testing.T) {
	withFakeGorefactor(t, `{"issues":[],"summary":{"total":0,"failing":0}}`)

	got := runLintAdvisory(t.TempDir())
	if got != "" {
		t.Errorf("expected empty advisory with 0 issues, got: %q", got)
	}
}

func TestRunLintAdvisory_FormatsIssues(t *testing.T) {
	withFakeGorefactor(t, `{
		"issues": [
			{"file":"a.go","rule":"file-size","severity":"warning","message":"oversized"},
			{"file":"b.go","rule":"missing-godoc","severity":"info","message":"no doc"}
		],
		"summary":{"total":2,"failing":0}
	}`)

	got := runLintAdvisory(t.TempDir())
	if got == "" {
		t.Fatal("expected non-empty advisory with 2 issues")
	}
	if !strings.Contains(got, "2 issue") {
		t.Errorf("advisory should mention issue count: %q", got)
	}
	if !strings.Contains(got, "file-size") {
		t.Errorf("advisory should mention rule names: %q", got)
	}
	if !strings.Contains(got, "advisory") {
		t.Errorf("advisory should label itself as advisory: %q", got)
	}
}

func TestRunLintAdvisory_TruncatesLongList(t *testing.T) {
	// More than 5 issues: only the first 5 should appear inline + "N more".
	issues := ""
	for i := 0; i < 8; i++ {
		if i > 0 {
			issues += ","
		}
		issues += fmt.Sprintf(
			`{"file":"f%d.go","rule":"rule%d","severity":"warning","message":"msg%d"}`, i, i, i)
	}
	withFakeGorefactor(t, fmt.Sprintf(`{"issues":[%s],"summary":{"total":8,"failing":0}}`, issues))

	got := runLintAdvisory(t.TempDir())
	if !strings.Contains(got, "and 3 more") {
		t.Errorf("advisory should note truncated issues: %q", got)
	}
}

// --- dispatch new tool routing ----------------------------------------------

// TestDispatch_NewToolsReturnContinue verifies every new tool name added in
// Phase 3 is recognized by dispatchTool (returns stContinue, not "unknown tool").
func TestDispatch_NewToolsReturnContinue(t *testing.T) {
	cfg := Config{Dir: "."}
	gf := 0

	cases := []struct {
		name string
		args string
	}{
		{"inspect_file", `{"file":"nonexistent.go"}`},
		{"skeleton", `{"file":"nonexistent.go"}`},
		{"review_changes", `{"ref":"HEAD"}`},
		{"lint_path", `{"path":"."}`},
		{"split_file", `{"file":"nonexistent.go"}`},
		{"wrap_errors", `{"file":"nonexistent.go","function":"Foo"}`},
		{"set_doc", `{"file":"nonexistent.go","declaration":"Foo","doc":"Foo does something."}`},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var call toolCall
			call.Function.Name = c.name
			call.Function.Arguments = c.args

			result, status := dispatchTool(call, cfg, &gf)

			if status != stContinue {
				t.Errorf("%s returned status %d, want stContinue (%d)",
					c.name, status, stContinue)
			}
			if strings.Contains(result, "unknown tool") {
				t.Errorf("%s returned 'unknown tool': %q", c.name, result)
			}
		})
	}
}

// TestDispatch_FinishIncludesLintAdvisory verifies that when build+test pass,
// the finish tool response may include an advisory lint summary (or nothing
// if no issues — both are acceptable; what matters is it does not crash and
// returns stSuccess on a green gate).
func TestDispatch_FinishGreenGate(t *testing.T) {
	dir := newSampleRepo(t) // clean repo with passing tests

	prev, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(prev)

	gf := 0
	var call toolCall
	call.Function.Name = "finish"
	call.Function.Arguments = `{}`

	result, status := dispatchTool(call, Config{Dir: dir}, &gf)
	if status != stSuccess {
		t.Fatalf("finish on green repo: status=%d result=%q", status, result)
	}
	if !strings.Contains(result, "gate green") {
		t.Errorf("finish result should confirm gate green: %q", result)
	}
}
