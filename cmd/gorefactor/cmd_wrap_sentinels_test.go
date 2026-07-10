package main

import (
	"strings"
	"testing"
)

const wrapSentinelsSrc = `package main

import "errors"

var ErrNotFound = errors.New("not found")

func LookupUser(id string) error {
	if id == "" {
		return ErrNotFound
	}
	return nil
}

func LookupOrder(id string) (string, error) {
	if id == "" {
		return "", ErrNotFound
	}
	return id, nil
}

func main() {}
`

func TestWrapSentinelsWrapsBareReturns(t *testing.T) {
	writeModule(t, map[string]string{"main.go": wrapSentinelsSrc})
	captureStdout(t, func() {
		if err := wrapSentinelsCommand([]string{"main.go", "ErrNotFound"}); err != nil {
			t.Fatalf("wrap-sentinels: %v", err)
		}
	})
	src := readFile(t, "main.go")
	if !strings.Contains(src, `return fmt.Errorf("lookup user: %w", ErrNotFound)`) {
		t.Errorf("LookupUser return should be wrapped with function context:\n%s", src)
	}
	if !strings.Contains(src, `return "", fmt.Errorf("lookup order: %w", ErrNotFound)`) {
		t.Errorf("multi-result return should wrap only the sentinel:\n%s", src)
	}
	// goimports must have added fmt for the new wraps.
	if !strings.Contains(src, `"fmt"`) {
		t.Errorf("fmt import missing after wrap:\n%s", src)
	}
	// The declaration itself is untouched.
	if !strings.Contains(src, `var ErrNotFound = errors.New("not found")`) {
		t.Errorf("sentinel declaration must be preserved:\n%s", src)
	}
}

func TestWrapSentinelsRejectsUnknownSentinel(t *testing.T) {
	writeModule(t, map[string]string{"main.go": wrapSentinelsSrc})
	if err := wrapSentinelsCommand([]string{"main.go", "ErrBogus"}); err == nil {
		t.Fatal("unknown sentinel must be rejected")
	}
}

func TestWrapSentinelsIdempotent(t *testing.T) {
	writeModule(t, map[string]string{"main.go": wrapSentinelsSrc})
	captureStdout(t, func() {
		if err := wrapSentinelsCommand([]string{"main.go", "ErrNotFound"}); err != nil {
			t.Fatalf("first run: %v", err)
		}
	})
	first := readFile(t, "main.go")
	out := captureStdout(t, func() {
		if err := wrapSentinelsCommand([]string{"main.go", "ErrNotFound"}); err != nil {
			t.Fatalf("second run: %v", err)
		}
	})
	if readFile(t, "main.go") != first {
		t.Error("second run must not change the file")
	}
	if !strings.Contains(out, "nothing to fix") {
		t.Errorf("second run should report nothing to fix, got: %s", out)
	}
}
