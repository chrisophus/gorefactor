package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chrisophus/gorefactor/orchestrator"
)

func TestDeleteCommandMissingTarget(t *testing.T) {
	t.Chdir(t.TempDir())
	path := writeTempGo(t, ".", "f.go", "package x\n\nfunc Keep() {}\n\nfunc Other() {}\n")
	before := readFile(t, path)

	for _, args := range [][]string{
		{path, "nonexistentFunc"},
		{path, "nonexistentFunc", "--safe"},
	} {
		err := deleteCommand(args)
		assertExitCode(t, err, exitNotFound)
		if !strings.Contains(err.Error(), `"nonexistentFunc" not found`) {
			t.Fatalf("error should name the missing target: %v", err)
		}
		if !strings.Contains(err.Error(), "Keep") || !strings.Contains(err.Error(), "Other") {
			t.Fatalf("error should list available declarations: %v", err)
		}
	}
	if readFile(t, path) != before {
		t.Fatal("file must not change on failed delete")
	}
	if entries, _ := orchestrator.LoadJournal(); len(entries) != 0 {
		t.Fatal("failed delete must not be journaled")
	}
}

func TestDeleteCommandDidYouMean(t *testing.T) {
	t.Chdir(t.TempDir())
	path := writeTempGo(t, ".", "f.go", "package x\n\nfunc Hello() {}\n")
	err := deleteCommand([]string{path, "Helo"})
	assertExitCode(t, err, exitNotFound)
	if !strings.Contains(err.Error(), `did you mean "Hello"?`) {
		t.Fatalf("expected did-you-mean hint, got: %v", err)
	}
}

func TestRenameCommandMissingSymbol(t *testing.T) {
	t.Chdir(t.TempDir())
	// Types-aware rename loads the package with go/packages, which needs a module.
	writeTempGo(t, ".", "go.mod", "module x\n\ngo 1.21\n")
	path := writeTempGo(t, ".", "f.go", "package x\n\nfunc helper() int { return 1 }\n")
	before := readFile(t, path)

	err := renameCommand([]string{path, "missingSym", "newName"})
	assertExitCode(t, err, exitNotFound)
	if !strings.Contains(err.Error(), `"missingSym" not found`) {
		t.Fatalf("error should name the missing symbol: %v", err)
	}
	if !strings.Contains(err.Error(), "helper") {
		t.Fatalf("error should list available symbols: %v", err)
	}
	if readFile(t, path) != before {
		t.Fatal("file must not change on failed rename")
	}
}

func TestStrictArgParsing(t *testing.T) {
	t.Chdir(t.TempDir())
	path := writeTempGo(t, ".", "f.go", "package x\n\nfunc add(a, b int) int {\n\treturn a + b\n}\n")
	before := readFile(t, path)

	cases := [][]string{
		{"replace-text", path, "add", "a + b", "a + b + 1", "--bogus"}, // unknown flag
		{"replace-text", path, "add", "a + b", "a + b + 1", "extra"},   // extra positional
		{"delete", path},                   // too few args
		{"delete", path, "add", "surplus"}, // too many args
		{"insert", path, "after:add", "func x() {}", "--unknown-thing"}, // unknown flag
	}
	for _, argv := range cases {
		cmd, ok := getCommands()[argv[0]]
		if !ok {
			t.Fatalf("command %s not registered", argv[0])
		}
		err := checkCommandArgs(cmd, argv[1:])
		if err == nil {
			t.Fatalf("expected usage error for %v", argv)
		}
		assertExitCode(t, err, exitUsage)
		if !strings.Contains(err.Error(), "usage: gorefactor "+argv[0]) {
			t.Fatalf("usage error should show command usage, got: %v", err)
		}
	}
	if readFile(t, path) != before {
		t.Fatal("strict arg rejection must not mutate the file")
	}
}

func TestRunMainExitCodes(t *testing.T) {
	t.Chdir(t.TempDir())
	path := writeTempGo(t, ".", "f.go", "package x\n\nfunc Hi() {}\n")

	if code := runMain([]string{"no-such-command"}); code != exitUsage {
		t.Fatalf("unknown command: exit = %d, want %d", code, exitUsage)
	}
	if code := runMain([]string{"delete", path, "Hi", "--bogus"}); code != exitUsage {
		t.Fatalf("unknown flag: exit = %d, want %d", code, exitUsage)
	}
	if code := runMain([]string{"delete", path, "Missing"}); code != exitNotFound {
		t.Fatalf("missing target: exit = %d, want %d", code, exitNotFound)
	}
	if code := runMain([]string{"insert", path, "at-end", "func Broken( {"}); code != exitParseError {
		t.Fatalf("bad snippet: exit = %d, want %d", code, exitParseError)
	}
	if code := runMain([]string{"delete", path, "Hi"}); code != exitOK {
		t.Fatalf("valid delete: exit = %d, want %d", code, exitOK)
	}
}

func TestUndoJournalOrdering(t *testing.T) {
	t.Chdir(t.TempDir())
	fileA := writeTempGo(t, ".", "a.go", "package x\n\nfunc A() int { return 1 }\n")
	fileB := writeTempGo(t, ".", "b.go", "package x\n\nfunc B() int { return 2 }\n")

	// Undo with empty journal errors.
	if err := undoRefactoring(nil); err == nil || !strings.Contains(err.Error(), "journal is empty") {
		t.Fatalf("expected empty-journal error, got %v", err)
	}

	// Mutation A then mutation B.
	if err := replaceTextCommand([]string{fileA, "A", "return 1", "return 10"}); err != nil {
		t.Fatalf("mutation A: %v", err)
	}
	if err := replaceTextCommand([]string{fileB, "B", "return 2", "return 20"}); err != nil {
		t.Fatalf("mutation B: %v", err)
	}

	// First undo restores B (most recent), not A.
	if err := undoRefactoring(nil); err != nil {
		t.Fatalf("undo: %v", err)
	}
	if !strings.Contains(readFile(t, fileB), "return 2") || strings.Contains(readFile(t, fileB), "return 20") {
		t.Fatal("first undo should restore the most recent mutation (B)")
	}
	if !strings.Contains(readFile(t, fileA), "return 10") {
		t.Fatal("first undo must not touch the earlier mutation (A)")
	}

	// Second undo restores A.
	if err := undoRefactoring(nil); err != nil {
		t.Fatalf("undo 2: %v", err)
	}
	if !strings.Contains(readFile(t, fileA), "return 1") || strings.Contains(readFile(t, fileA), "return 10") {
		t.Fatal("second undo should restore A")
	}

	// Journal is empty again.
	if err := undoRefactoring(nil); err == nil || !strings.Contains(err.Error(), "journal is empty") {
		t.Fatalf("expected empty-journal error after popping all entries, got %v", err)
	}
}

func TestUndoCreateRemovesFile(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := createCommand([]string{"new.go", "package x\n\nfunc N() {}\n"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := undoRefactoring(nil); err != nil {
		t.Fatalf("undo: %v", err)
	}
	if _, err := os.Stat("new.go"); !os.IsNotExist(err) {
		t.Fatal("undo of create should delete the created file")
	}
}

func TestMutationJSONOutputShape(t *testing.T) {
	t.Chdir(t.TempDir())
	path := writeTempGo(t, ".", "f.go", "package x\n\nfunc Drop() {}\n\nfunc Keep() {}\n")

	out := captureStdout(t, func() {
		if err := deleteCommand([]string{path, "Drop", "--json"}); err != nil {
			t.Errorf("delete --json: %v", err)
		}
	})
	var res mutationResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if !res.Success || res.Operation != "delete" || res.File != path {
		t.Fatalf("unexpected result: %+v", res)
	}
	if res.UndoToken == "" {
		t.Fatal("success result must carry an undoToken")
	}
	if res.LinesChanged == 0 {
		t.Fatal("linesChanged should be non-zero for a delete")
	}

	// Error shape: success=false, error message, candidates, still non-zero.
	var jerr error
	out = captureStdout(t, func() {
		jerr = deleteCommand([]string{path, "Nope", "--json"})
	})
	assertExitCode(t, jerr, exitNotFound)
	var eres mutationResult
	if err := json.Unmarshal([]byte(out), &eres); err != nil {
		t.Fatalf("error output is not valid JSON: %v\n%s", err, out)
	}
	if eres.Success || eres.Error == "" {
		t.Fatalf("unexpected error result: %+v", eres)
	}
	if len(eres.Candidates) == 0 || eres.Candidates[0] != "Keep" {
		t.Fatalf("error result should list candidates, got %+v", eres.Candidates)
	}
}

func TestDryRunDoesNotWriteOrJournal(t *testing.T) {
	t.Chdir(t.TempDir())
	path := writeTempGo(t, ".", "f.go", "package x\n\nfunc add(a, b int) int {\n\treturn a + b\n}\n")
	before := readFile(t, path)

	out := captureStdout(t, func() {
		if err := replaceTextCommand([]string{path, "add", "a + b", "a * b", "--dry-run"}); err != nil {
			t.Errorf("dry-run: %v", err)
		}
	})
	if readFile(t, path) != before {
		t.Fatal("--dry-run must not modify the file")
	}
	if entries, _ := orchestrator.LoadJournal(); len(entries) != 0 {
		t.Fatal("--dry-run must not journal")
	}
	if !strings.Contains(out, "-\treturn a + b") || !strings.Contains(out, "+\treturn a * b") {
		t.Fatalf("dry-run should print a unified diff, got:\n%s", out)
	}

	// JSON dry-run carries the diff in the result struct.
	out = captureStdout(t, func() {
		if err := replaceTextCommand([]string{path, "add", "a + b", "a * b", "--dry-run", "--json"}); err != nil {
			t.Errorf("dry-run json: %v", err)
		}
	})
	var res mutationResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if !res.DryRun || res.Diff == "" || res.UndoToken != "" {
		t.Fatalf("unexpected dry-run result: %+v", res)
	}
}

func TestReplaceTextOccurrenceControl(t *testing.T) {
	t.Chdir(t.TempDir())
	src := "package x\n\nfunc f() int {\n\tn := 1\n\tn = n + 1\n\tn = n + 1\n\treturn n\n}\n"

	path := writeTempGo(t, ".", "first.go", src)
	out := captureStdout(t, func() {
		if err := replaceTextCommand([]string{path, "f", "n + 1", "n + 7", "--first"}); err != nil {
			t.Errorf("--first: %v", err)
		}
	})
	got := readFile(t, path)
	if strings.Count(got, "n + 7") != 1 || strings.Count(got, "n + 1") != 1 {
		t.Fatalf("--first should replace exactly one occurrence:\n%s", got)
	}
	if !strings.Contains(out, "Replaced occurrence 1 of 2") {
		t.Fatalf("message should state occurrence and total, got: %s", out)
	}

	path2 := writeTempGo(t, ".", "second.go", src)
	if err := replaceTextCommand([]string{path2, "f", "n + 1", "n + 9", "--occurrence", "2"}); err != nil {
		t.Fatalf("--occurrence 2: %v", err)
	}
	got2 := readFile(t, path2)
	if !strings.Contains(got2, "n = n + 1\n\tn = n + 9") {
		t.Fatalf("--occurrence 2 should replace only the second occurrence:\n%s", got2)
	}

	// Out-of-range occurrence is a semantic miss (exit 2).
	err := replaceTextCommand([]string{path2, "f", "n + 9", "x", "--occurrence", "5"})
	assertExitCode(t, err, exitNotFound)

	// Default still replaces all and reports an accurate count.
	path3 := writeTempGo(t, ".", "all.go", src)
	out = captureStdout(t, func() {
		if err := replaceTextCommand([]string{path3, "f", "n + 1", "n + 3"}); err != nil {
			t.Errorf("replace all: %v", err)
		}
	})
	if !strings.Contains(out, "Replaced 2 occurrence(s)") {
		t.Fatalf("expected accurate count, got: %s", out)
	}
}

func TestInsertTargetNotFoundListsCandidates(t *testing.T) {
	t.Chdir(t.TempDir())
	path := writeTempGo(t, ".", "f.go", "package x\n\nfunc Alpha() {}\n\nfunc Beta() {}\n")
	err := insertCommand([]string{path, "after:Gamma", "func X() {}"})
	assertExitCode(t, err, exitNotFound)
	if !strings.Contains(err.Error(), "Alpha") || !strings.Contains(err.Error(), "Beta") {
		t.Fatalf("expected candidates in error, got: %v", err)
	}
}

func TestHistoryCommandListsJournal(t *testing.T) {
	t.Chdir(t.TempDir())
	path := writeTempGo(t, ".", "f.go", "package x\n\nfunc Drop() {}\n")
	if err := deleteCommand([]string{path, "Drop"}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	out := captureStdout(t, func() {
		if err := historyCommand(nil); err != nil {
			t.Errorf("history: %v", err)
		}
	})
	if !strings.Contains(out, "delete") || !strings.Contains(out, path) {
		t.Fatalf("history should list the journaled delete, got:\n%s", out)
	}

	out = captureStdout(t, func() {
		if err := historyCommand([]string{"--json"}); err != nil {
			t.Errorf("history --json: %v", err)
		}
	})
	var entries []orchestrator.JournalEntry
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("history --json output invalid: %v\n%s", err, out)
	}
	if len(entries) != 1 || entries[0].Command != "delete" {
		t.Fatalf("unexpected journal entries: %+v", entries)
	}
}

func TestGateRollsBackOnBuildFailure(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("go.mod", []byte("module gatetest\n\ngo 1.24\n"), 0644); err != nil {
		t.Fatal(err)
	}
	path := writeTempGo(t, ".", "f.go",
		"package main\n\nfunc helper() int { return 1 }\n\nfunc main() { _ = helper() }\n")
	before := readFile(t, path)

	err := deleteCommand([]string{path, "helper", "--gate"})
	assertExitCode(t, err, exitGateFailure)
	if readFile(t, path) != before {
		t.Fatal("--gate failure must roll the file back")
	}
	if entries, _ := orchestrator.LoadJournal(); len(entries) != 0 {
		t.Fatal("rolled-back operation must not remain in the journal")
	}
}

func TestExitCodeForClassification(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{nil, exitOK},
		{usageErrorf("bad args"), exitUsage},
		{notFoundErrorf("missing"), exitNotFound},
		{parseErrorf("syntax"), exitParseError},
		{gateErrorf("build broke"), exitGateFailure},
		{errors.New("plain"), exitUsage},
	}
	for _, c := range cases {
		if got := exitCodeFor(c.err); got != c.want {
			t.Errorf("exitCodeFor(%v) = %d, want %d", c.err, got, c.want)
		}
	}
}

func TestUnknownCommandSuggestion(t *testing.T) {
	if hint := closestMatch("renam", commandNames()); hint != "rename" {
		t.Fatalf("closestMatch(renam) = %q, want rename", hint)
	}
	if hint := closestMatch("zzzzzzz", commandNames()); hint != "" {
		t.Fatalf("closestMatch for nonsense should be empty, got %q", hint)
	}
}

func TestCreateRejectsBadGoContent(t *testing.T) {
	t.Chdir(t.TempDir())
	err := createCommand([]string{"bad.go", "func no package clause"})
	assertExitCode(t, err, exitParseError)
	if _, serr := os.Stat("bad.go"); !os.IsNotExist(serr) {
		t.Fatal("rejected create must not leave a file behind")
	}
}

func TestMoveMissingTargetListsCandidates(t *testing.T) {
	t.Chdir(t.TempDir())
	path := writeTempGo(t, ".", "src.go", "package x\n\nfunc Real() {}\n")
	err := moveCode([]string{path, "Fake", filepath.Join(".", "dst.go")})
	assertExitCode(t, err, exitNotFound)
	if !strings.Contains(err.Error(), "Real") {
		t.Fatalf("expected candidate list, got: %v", err)
	}
	if _, serr := os.Stat("dst.go"); !os.IsNotExist(serr) {
		t.Fatal("failed move must not create the destination file")
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()
	fn()
	_ = w.Close()
	b, _ := io.ReadAll(r)
	os.Stdout = old
	return string(b)
}

func assertExitCode(t *testing.T, err error, want int) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error with exit code %d, got nil", want)
	}
	if got := exitCodeFor(err); got != want {
		t.Fatalf("exit code = %d, want %d (err: %v)", got, want, err)
	}
}
