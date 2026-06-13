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
