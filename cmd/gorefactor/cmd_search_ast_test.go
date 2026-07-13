package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/chrisophus/gorefactor/analyzer"
)

const searchASTTestSrc = `package x

import "errors"

func a() error {
	err := errors.New("boom")
	if err != nil {
		return err
	}
	return nil
}

func b(x int) error {
	e := errors.New("other")
	if e != nil {
		return e
	}
	if x > 0 {
		return nil
	}
	return nil
}

func c() {
	println("formatting   differs")
	if true {
	}
}
`

func TestSearchASTWildcardStatement(t *testing.T) {
	t.Chdir(t.TempDir())
	writeTempGo(t, ".", "x.go", searchASTTestSrc)

	out := captureStdout(t, func() {
		if err := searchASTCommand([]string{"if $_ != nil { return $_ }"}); err != nil {
			t.Errorf("search-ast: %v", err)
		}
	})
	if !strings.Contains(out, "x.go:7") || !strings.Contains(out, "x.go:15") {
		t.Fatalf("expected matches at lines 7 and 15:\n%s", out)
	}
	if !strings.Contains(out, "2 match(es)") {
		t.Fatalf("expected exactly 2 matches:\n%s", out)
	}
	// the `if x > 0` block must not match
	if strings.Contains(out, "x.go:18") {
		t.Fatalf("structural mismatch should not match:\n%s", out)
	}
}

func TestSearchASTExpressionPattern(t *testing.T) {
	t.Chdir(t.TempDir())
	writeTempGo(t, ".", "x.go", searchASTTestSrc)

	out := captureStdout(t, func() {
		if err := searchASTCommand([]string{"errors.New($_)", "--json"}); err != nil {
			t.Errorf("search-ast --json: %v", err)
		}
	})
	var res struct {
		Pattern string              `json:"pattern"`
		Matches []analyzer.ASTMatch `json:"matches"`
		Total   int                 `json:"total"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if res.Total != 2 || len(res.Matches) != 2 {
		t.Fatalf("expected 2 matches, got %+v", res)
	}
	if res.Matches[0].Line != 6 || !strings.Contains(res.Matches[0].Snippet, `errors.New("boom")`) {
		t.Fatalf("unexpected first match: %+v", res.Matches[0])
	}
}

func TestSearchASTLiteralMatchIgnoresFormatting(t *testing.T) {
	t.Chdir(t.TempDir())
	writeTempGo(t, ".", "x.go", searchASTTestSrc)

	// extra whitespace in the pattern is irrelevant: AST-level match
	out := captureStdout(t, func() {
		if err := searchASTCommand([]string{"println(  \"formatting   differs\"  )"}); err != nil {
			t.Errorf("search-ast: %v", err)
		}
	})
	if !strings.Contains(out, "x.go:25") || !strings.Contains(out, "1 match(es)") {
		t.Fatalf("literal pattern should match despite formatting:\n%s", out)
	}
}

func TestSearchASTBadPattern(t *testing.T) {
	t.Chdir(t.TempDir())
	writeTempGo(t, ".", "x.go", searchASTTestSrc)

	err := searchASTCommand([]string{"if {{{"})
	assertExitCode(t, err, exitParseError)
}

func TestSearchASTNoMatchesIsOK(t *testing.T) {
	t.Chdir(t.TempDir())
	writeTempGo(t, ".", "x.go", searchASTTestSrc)

	out := captureStdout(t, func() {
		if err := searchASTCommand([]string{"os.Exit($_)"}); err != nil {
			t.Errorf("zero matches must not be an error: %v", err)
		}
	})
	if !strings.Contains(out, "0 match(es)") {
		t.Fatalf("expected zero-match summary:\n%s", out)
	}
}
