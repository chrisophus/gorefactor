package main

import (
	"encoding/json"
	"strings"
	"testing"
)

const wrapErrorsSrc = `package main

import "fmt"

func FetchUser(id string) (string, error) {
	result, err := loadFromDB(id)
	if err != nil {
		return "", err
	}
	return result, nil
}

func SaveItem(item string) error {
	err := persist(item)
	if err != nil {
		return err
	}
	return nil
}

func loadFromDB(id string) (string, error) { return id, nil }
func persist(s string) error               { return nil }

func main() {
	_ = fmt.Sprint("x")
}
`

func TestWrapErrorsTransformsBarrReturns(t *testing.T) {
	writeModule(t, map[string]string{"main.go": wrapErrorsSrc})
	out := captureStdout(t, func() {
		if err := wrapErrorsCommand([]string{"main.go"}); err != nil {
			t.Errorf("wrap-errors: %v", err)
		}
	})
	src := readFile(t, "main.go")
	// FetchUser: bare return "", err replaced with fmt.Errorf wrapping.
	if strings.Contains(src, "return \"\", err\n") {
		t.Fatalf("bare return err should have been wrapped in FetchUser:\n%s", src)
	}
	if !strings.Contains(src, "fmt.Errorf(") {
		t.Fatalf("fmt.Errorf not found after wrapping:\n%s", src)
	}
	// Should wrap SaveItem too.
	if strings.Contains(src, "\treturn err\n") {
		t.Fatalf("bare return err should have been wrapped in SaveItem:\n%s", src)
	}
	_ = out // summary printed to stdout
}

// TestWrapErrorsLeavesNilableBareReturnAlone locks in improvement plan item 4:
// wrap-errors must never wrap a bare `return x, err` that is NOT inside an
// `if err != nil` guard, because err may be nil there (e.g. the nil-on-success
// return of filepath.WalkDir). Wrapping a nil err with fmt.Errorf produces a
// non-nil error and silently breaks callers. wrap-errors only ever rewrites
// returns inside `if err != nil` blocks, where err is provably non-nil.
func TestWrapErrorsLeavesNilableBareReturnAlone(t *testing.T) {
	const src = `package main

import (
	"io/fs"
	"path/filepath"
)

func listFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		files = append(files, p)
		return nil
	})
	// err is nil on success — must NOT be wrapped.
	return files, err
}
`
	writeModule(t, map[string]string{"main.go": src})
	_ = captureStdout(t, func() {
		if err := wrapErrorsCommand([]string{"main.go"}); err != nil {
			t.Fatalf("wrap-errors: %v", err)
		}
	})
	got := readFile(t, "main.go")
	if !strings.Contains(got, "return files, err") {
		t.Fatalf("nil-able bare return was rewritten (would break on success):\n%s", got)
	}
	if strings.Contains(got, "fmt.Errorf") {
		t.Fatalf("no fmt.Errorf wrapping should have been introduced:\n%s", got)
	}
}

func TestWrapErrorsWithFuncFilter(t *testing.T) {
	writeModule(t, map[string]string{"main.go": wrapErrorsSrc})
	if err := wrapErrorsCommand([]string{"main.go", "SaveItem"}); err != nil {
		t.Fatalf("wrap-errors --func: %v", err)
	}
	src := readFile(t, "main.go")
	// SaveItem should be wrapped.
	if strings.Contains(src, "\treturn err\n") {
		t.Fatalf("SaveItem bare return err should be wrapped:\n%s", src)
	}
	// FetchUser should be untouched.
	if !strings.Contains(src, "return \"\", err") {
		t.Fatalf("FetchUser should be untouched when filter is SaveItem:\n%s", src)
	}
}

func TestWrapErrorsDryRunDoesNotWrite(t *testing.T) {
	writeModule(t, map[string]string{"main.go": wrapErrorsSrc})
	before := readFile(t, "main.go")
	if err := wrapErrorsCommand([]string{"main.go", "--dry-run"}); err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if readFile(t, "main.go") != before {
		t.Fatal("--dry-run must not write the file")
	}
}

func TestWrapErrorsJSONOutput(t *testing.T) {
	writeModule(t, map[string]string{"main.go": wrapErrorsSrc})
	out := captureStdout(t, func() {
		if err := wrapErrorsCommand([]string{"main.go", "--json"}); err != nil {
			t.Errorf("--json: %v", err)
		}
	})
	var res mutationResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("output not valid JSON: %v\n%s", err, out)
	}
	if !res.Success {
		t.Fatalf("expected success=true, got: %+v", res)
	}
}

func TestWrapErrorsAlreadyWrappedIsSkipped(t *testing.T) {
	src := `package main

import "fmt"

func FetchUser(id string) (string, error) {
	result, err := loadFromDB(id)
	if err != nil {
		return "", fmt.Errorf("load from db: %w", err)
	}
	return result, nil
}

func loadFromDB(id string) (string, error) { return id, nil }
`
	writeModule(t, map[string]string{"main.go": src})
	before := readFile(t, "main.go")
	if err := wrapErrorsCommand([]string{"main.go"}); err != nil {
		t.Fatalf("wrap-errors on already-wrapped: %v", err)
	}
	// Already wrapped — the return has fmt.Errorf, not bare err ident,
	// so the bare-err check won't match and the file is unchanged.
	if readFile(t, "main.go") != before {
		t.Fatal("already-wrapped file should not be modified")
	}
}

func TestWrapErrorsMultiStatementBodySkipped(t *testing.T) {
	src := `package main

import "log"

func FetchUser(id string) (string, error) {
	result, err := loadFromDB(id)
	if err != nil {
		log.Printf("error: %v", err)
		return "", err
	}
	return result, nil
}

func loadFromDB(id string) (string, error) { return id, nil }
`
	writeModule(t, map[string]string{"main.go": src})
	before := readFile(t, "main.go")
	out := captureStdout(t, func() {
		if err := wrapErrorsCommand([]string{"main.go"}); err != nil {
			t.Errorf("wrap-errors: %v", err)
		}
	})
	// Multi-statement if body → skip.
	if readFile(t, "main.go") != before {
		t.Fatal("multi-statement if body should not be modified")
	}
	if !strings.Contains(out, "skipped") {
		t.Fatalf("output should mention skipped, got: %s", out)
	}
}

func TestWrapErrorsContextFromCall(t *testing.T) {
	src := `package main

func LoadUser(id string) (string, error) {
	user, err := fetchFromDatabase(id)
	if err != nil {
		return "", err
	}
	return user, nil
}

func fetchFromDatabase(id string) (string, error) { return id, nil }
`
	writeModule(t, map[string]string{"main.go": src})
	if err := wrapErrorsCommand([]string{"main.go"}); err != nil {
		t.Fatalf("wrap-errors: %v", err)
	}
	got := readFile(t, "main.go")
	// Context should be derived from the function name "fetchFromDatabase".
	if !strings.Contains(got, "fetch from database") {
		t.Fatalf("context not derived from preceding call name:\n%s", got)
	}
}

// TestWrapErrorsSentinelBranchBeforeBareReturn is a regression test for Bug 1:
// wrap-errors was skipping entire if err != nil blocks whenever the body
// contained more than one statement, even when the extra statements were
// safe sentinel branches (errors.Is checks) that returned nil for the error
// slot. The fix scopes the check to the individual block.
func TestWrapErrorsSentinelBranchBeforeBareReturn(t *testing.T) {
	src := `package main

import (
	"errors"
	"fmt"
)

var ErrNotFound = errors.New("not found")

func errNotFound(msg string) error { return fmt.Errorf("%s", msg) }

func DeleteGrant(id string) (string, error) {
	result, err := doDelete(id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return errNotFound("not found").Error(), nil
		}
		return "", err
	}
	return result, nil
}

func doDelete(id string) (string, error) { return id, nil }
`
	writeModule(t, map[string]string{"main.go": src})
	out := captureStdout(t, func() {
		if err := wrapErrorsCommand([]string{"main.go"}); err != nil {
			t.Fatalf("wrap-errors: %v", err)
		}
	})
	got := readFile(t, "main.go")
	// The bare `return "", err` should have been wrapped.
	if strings.Contains(got, "return \"\", err\n") {
		t.Fatalf("bare return err after sentinel branch should have been wrapped;\nout=%s\nsrc=%s", out, got)
	}
	if !strings.Contains(got, "fmt.Errorf(") {
		t.Fatalf("expected fmt.Errorf wrapping; got:\n%s", got)
	}
}

// TestWrapErrorsLoopBareReturn is a regression test for Bug 1: bare
// `return nil, err` inside a for-loop's if err != nil block was skipped
// because the function contained multiple return statements elsewhere.
func TestWrapErrorsLoopBareReturn(t *testing.T) {
	src := `package main

func ProcessItems(ids []string) ([]string, error) {
	var results []string
	for _, id := range ids {
		res, err := process(id)
		if err != nil {
			return nil, err
		}
		results = append(results, res)
	}
	return results, nil
}

func process(id string) (string, error) { return id, nil }
`
	writeModule(t, map[string]string{"main.go": src})
	if err := wrapErrorsCommand([]string{"main.go"}); err != nil {
		t.Fatalf("wrap-errors: %v", err)
	}
	got := readFile(t, "main.go")
	// The bare `return nil, err` inside the loop should have been wrapped.
	if strings.Contains(got, "return nil, err\n") {
		t.Fatalf("bare return nil, err inside loop should have been wrapped:\n%s", got)
	}
	if !strings.Contains(got, "fmt.Errorf(") {
		t.Fatalf("expected fmt.Errorf wrapping; got:\n%s", got)
	}
}

// TestWrapErrorsDocCommentNotEmbedded is a regression test for Bug 2:
// when the last statement in a function is `return nil, err` and the very
// next line after the closing brace is a doc comment for the following
// function, the rewriter was embedding that doc comment inside the
// fmt.Errorf(...) call and removing it from its correct position.
func TestWrapErrorsDocCommentNotEmbedded(t *testing.T) {
	src := `package main

// FirstFunc does something.
func FirstFunc() (string, error) {
	result, err := helper()
	if err != nil {
		return "", err
	}
	return result, nil
}

// SecondFunc does something important.
func SecondFunc() string { return "ok" }

func helper() (string, error) { return "x", nil }
`
	writeModule(t, map[string]string{"main.go": src})
	if err := wrapErrorsCommand([]string{"main.go"}); err != nil {
		t.Fatalf("wrap-errors: %v", err)
	}
	got := readFile(t, "main.go")
	// The doc comment for SecondFunc must NOT appear inside fmt.Errorf.
	if strings.Contains(got, "Errorf(") && strings.Contains(got, "SecondFunc does something important") {
		// Check whether the comment ended up inside the Errorf call.
		errorfIdx := strings.Index(got, "Errorf(")
		commentIdx := strings.Index(got, "SecondFunc does something important")
		closingIdx := strings.Index(got[errorfIdx:], ")")
		if commentIdx > errorfIdx && commentIdx < errorfIdx+closingIdx {
			t.Fatalf("doc comment for SecondFunc was embedded inside fmt.Errorf:\n%s", got)
		}
	}
	// The doc comment must still be present before SecondFunc.
	if !strings.Contains(got, "// SecondFunc does something important.") {
		t.Fatalf("doc comment for SecondFunc was lost after rewrite:\n%s", got)
	}
	// FirstFunc should have been transformed.
	if strings.Contains(got, "return \"\", err\n") {
		t.Fatalf("bare return err in FirstFunc should have been wrapped:\n%s", got)
	}
}

func TestWrapErrorsContextFallsBackToFuncName(t *testing.T) {
	// When no preceding assignment precedes the if block, use the function name.
	src := `package main

var globalErr error

func ProcessGlobal() (string, error) {
	if globalErr != nil {
		return "", globalErr
	}
	return "ok", nil
}
`
	writeModule(t, map[string]string{"main.go": src})
	// globalErr is not a call-result assignment, so context must fall back.
	// Also note: this uses globalErr not err, so isErrNotNil should be false → skip.
	before := readFile(t, "main.go")
	if err := wrapErrorsCommand([]string{"main.go"}); err != nil {
		t.Fatalf("wrap-errors: %v", err)
	}
	// Should be a no-op because condition is `globalErr != nil`, not `err != nil`.
	if readFile(t, "main.go") != before {
		t.Fatalf("non-err condition should not be modified:\n%s", readFile(t, "main.go"))
	}
}
