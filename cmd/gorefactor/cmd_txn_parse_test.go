package main

import (
	"os"
	"path/filepath"
	"testing"
)

// ---- splitCommandLine ----

func TestSplitCommandLine_SimpleTokens(t *testing.T) {
	t.Parallel()
	got, err := splitCommandLine("insert foo.go at-end")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"insert", "foo.go", "at-end"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d; got %v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("token[%d] = %q, want %q", i, got[i], w)
		}
	}
}

func TestSplitCommandLine_SingleQuotes(t *testing.T) {
	t.Parallel()
	got, err := splitCommandLine("replace file.go 'old stmt' 'new stmt'")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("len = %d, want 4; got %v", len(got), got)
	}
	if got[2] != "old stmt" || got[3] != "new stmt" {
		t.Errorf("quoted tokens: %v", got)
	}
}

func TestSplitCommandLine_DoubleQuotes(t *testing.T) {
	t.Parallel()
	got, err := splitCommandLine(`rename file.go "old name" "new name"`)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("len = %d, want 4; got %v", len(got), got)
	}
	if got[2] != "old name" || got[3] != "new name" {
		t.Errorf("double-quoted tokens: %v", got)
	}
}

func TestSplitCommandLine_BackslashEscapeInDoubleQuotes(t *testing.T) {
	t.Parallel()
	// Backslash inside double quotes escapes the next character.
	got, err := splitCommandLine(`cmd "say \"hi\""`)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2; got %v", len(got), got)
	}
	if got[1] != `say "hi"` {
		t.Errorf("escaped token = %q, want %q", got[1], `say "hi"`)
	}
}

func TestSplitCommandLine_BackslashEscapeOutsideQuotes(t *testing.T) {
	t.Parallel()
	got, err := splitCommandLine(`cmd arg\ with\ spaces`)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2; got %v", len(got), got)
	}
	if got[1] != "arg with spaces" {
		t.Errorf("escaped token = %q, want %q", got[1], "arg with spaces")
	}
}

func TestSplitCommandLine_UnterminatedSingleQuote(t *testing.T) {
	t.Parallel()
	_, err := splitCommandLine("cmd 'unterminated")
	if err == nil {
		t.Fatal("expected error for unterminated single quote")
	}
}

func TestSplitCommandLine_UnterminatedDoubleQuote(t *testing.T) {
	t.Parallel()
	_, err := splitCommandLine(`cmd "unterminated`)
	if err == nil {
		t.Fatal("expected error for unterminated double quote")
	}
}

func TestSplitCommandLine_EmptyString(t *testing.T) {
	t.Parallel()
	got, err := splitCommandLine("")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty slice for empty input, got %v", got)
	}
}

func TestSplitCommandLine_Tabs(t *testing.T) {
	t.Parallel()
	got, err := splitCommandLine("cmd\targ1\targ2")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3; got %v", len(got), got)
	}
}

func TestSplitCommandLine_AdjacentQuotedSegments(t *testing.T) {
	t.Parallel()
	// Adjacent quoted and unquoted segments merge into one token.
	got, err := splitCommandLine(`cmd foo'bar'baz`)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2; got %v", len(got), got)
	}
	if got[1] != "foobarbaz" {
		t.Errorf("merged token = %q, want %q", got[1], "foobarbaz")
	}
}

// ---- parseTxnScript ----

func TestParseTxnScript_BlankLinesAndComments(t *testing.T) {
	t.Parallel()
	script := `
# This is a comment
  # indented comment

insert foo.go at-end

# another comment
`
	lines, err := parseTxnScript(script)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 {
		t.Fatalf("expected 1 command line, got %d: %v", len(lines), lines)
	}
	if lines[0].argv[0] != "insert" {
		t.Errorf("expected command 'insert', got %q", lines[0].argv[0])
	}
}

func TestParseTxnScript_MultipleCommands(t *testing.T) {
	t.Parallel()
	script := `insert foo.go at-end
rename foo.go OldName NewName
delete bar.go MyFunc`
	lines, err := parseTxnScript(script)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %v", len(lines), lines)
	}
	wantCmds := []string{"insert", "rename", "delete"}
	for i, want := range wantCmds {
		if lines[i].argv[0] != want {
			t.Errorf("line[%d].argv[0] = %q, want %q", i, lines[i].argv[0], want)
		}
	}
}

func TestParseTxnScript_LineNumbersCorrect(t *testing.T) {
	t.Parallel()
	script := "# comment\n\ncreate new.go\ninsert new.go at-end"
	lines, err := parseTxnScript(script)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	// "create" is on source line 3 (1-indexed), "insert" on line 4.
	if lines[0].line != 3 {
		t.Errorf("create line = %d, want 3", lines[0].line)
	}
	if lines[1].line != 4 {
		t.Errorf("insert line = %d, want 4", lines[1].line)
	}
}

func TestParseTxnScript_SyntaxErrorReturnsError(t *testing.T) {
	t.Parallel()
	script := `insert "unterminated`
	_, err := parseTxnScript(script)
	if err == nil {
		t.Fatal("expected error for syntax error in script")
	}
}

func TestParseTxnScript_EmptyScript(t *testing.T) {
	t.Parallel()
	lines, err := parseTxnScript("")
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 0 {
		t.Errorf("expected empty result for empty script, got %v", lines)
	}
}

// ---- txnCollector ----

func TestTxnCollector_RecordBeforeState(t *testing.T) {
	t.Parallel()
	c := newTxnCollector()

	c.record(map[string][]byte{
		"a.go": []byte("content-a"),
		"b.go": []byte("content-b"),
	}, nil)

	if string(c.before["a.go"]) != "content-a" {
		t.Errorf("before[a.go] = %q", c.before["a.go"])
	}
	if string(c.before["b.go"]) != "content-b" {
		t.Errorf("before[b.go] = %q", c.before["b.go"])
	}
}

func TestTxnCollector_FirstStateWins(t *testing.T) {
	t.Parallel()
	c := newTxnCollector()

	c.record(map[string][]byte{"a.go": []byte("original")}, nil)
	// Second record should not overwrite the first.
	c.record(map[string][]byte{"a.go": []byte("changed")}, nil)

	if string(c.before["a.go"]) != "original" {
		t.Errorf("expected first state to win, got %q", c.before["a.go"])
	}
}

func TestTxnCollector_TracksCreatedFiles(t *testing.T) {
	t.Parallel()
	c := newTxnCollector()

	c.record(nil, []string{"new.go", "another.go"})

	if !c.created["new.go"] {
		t.Error("expected new.go in created set")
	}
	if !c.created["another.go"] {
		t.Error("expected another.go in created set")
	}
}

func TestTxnCollector_Touched_ReturnsSorted(t *testing.T) {
	t.Parallel()
	c := newTxnCollector()
	c.record(map[string][]byte{
		"z.go": nil,
		"a.go": nil,
		"m.go": nil,
	}, nil)

	paths := c.touched()
	if len(paths) != 3 {
		t.Fatalf("expected 3 paths, got %d: %v", len(paths), paths)
	}
	for i := 1; i < len(paths); i++ {
		if paths[i] < paths[i-1] {
			t.Errorf("paths not sorted: %v", paths)
		}
	}
}

func TestTxnCollector_Restore_WritesAndDeletes(t *testing.T) {
	// os.WriteFile and os.Remove need real paths; can't run in parallel.
	dir := t.TempDir()

	existingFile := filepath.Join(dir, "existing.go")
	if err := os.WriteFile(existingFile, []byte("new content"), 0644); err != nil {
		t.Fatal(err)
	}
	createdFile := filepath.Join(dir, "created.go")
	if err := os.WriteFile(createdFile, []byte("created"), 0644); err != nil {
		t.Fatal(err)
	}

	c := newTxnCollector()
	c.record(map[string][]byte{existingFile: []byte("original content")}, []string{createdFile})

	c.restore()

	// existing.go should be restored to original content.
	got, err := os.ReadFile(existingFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "original content" {
		t.Errorf("restored content = %q, want %q", got, "original content")
	}

	// created.go should be removed.
	if _, err := os.Stat(createdFile); !os.IsNotExist(err) {
		t.Error("expected created.go to be removed by restore")
	}
}

// ---- readTxnScript ----

func TestReadTxnScript_FromFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "script.txn")
	content := "insert foo.go at-end\n"
	if err := os.WriteFile(scriptPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := readTxnScript([]string{scriptPath})
	if err != nil {
		t.Fatal(err)
	}
	if got != content {
		t.Errorf("readTxnScript = %q, want %q", got, content)
	}
}

func TestReadTxnScript_MissingFile(t *testing.T) {
	t.Parallel()
	_, err := readTxnScript([]string{"/nonexistent/path/script.txn"})
	if err == nil {
		t.Fatal("expected error for missing script file")
	}
}
