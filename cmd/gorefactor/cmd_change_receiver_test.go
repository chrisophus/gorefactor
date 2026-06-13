package main

import (
	"strings"
	"testing"
)

const counterSrc = "package x\n\ntype Counter struct{ n int }\n\nfunc (c Counter) Get() int { return c.n }\n\nfunc (c *Counter) Inc() { c.n++ }\n"

func TestChangeReceiverToPointer(t *testing.T) {
	t.Chdir(t.TempDir())
	path := writeTempGo(t, ".", "f.go", counterSrc)

	if err := changeReceiverCommand([]string{path, "Counter:Get", "--pointer"}); err != nil {
		t.Fatalf("change-receiver: %v", err)
	}
	got := readFile(t, path)
	if !strings.Contains(got, "func (c *Counter) Get() int") {
		t.Fatalf("receiver should be pointer:\n%s", got)
	}
	// Undoable via the journal.
	if err := undoRefactoring(nil); err != nil {
		t.Fatalf("undo: %v", err)
	}
	if !strings.Contains(readFile(t, path), "func (c Counter) Get() int") {
		t.Fatal("undo should restore the value receiver")
	}
}

func TestChangeReceiverToValue(t *testing.T) {
	t.Chdir(t.TempDir())
	path := writeTempGo(t, ".", "f.go", counterSrc)

	if err := changeReceiverCommand([]string{path, "Counter:Inc", "--value"}); err != nil {
		t.Fatalf("change-receiver: %v", err)
	}
	if !strings.Contains(readFile(t, path), "func (c Counter) Inc()") {
		t.Fatalf("receiver should be value:\n%s", readFile(t, path))
	}
}

func TestChangeReceiverNoOpWhenAlreadyInForm(t *testing.T) {
	t.Chdir(t.TempDir())
	path := writeTempGo(t, ".", "f.go", counterSrc)
	before := readFile(t, path)

	out := captureStdout(t, func() {
		if err := changeReceiverCommand([]string{path, "Counter:Inc", "--pointer"}); err != nil {
			t.Errorf("no-op change: %v", err)
		}
	})
	if readFile(t, path) != before {
		t.Fatal("no-op must not modify the file")
	}
	if !strings.Contains(out, "already has a pointer receiver") {
		t.Fatalf("expected no-op message, got: %s", out)
	}
}

func TestChangeReceiverFlagValidation(t *testing.T) {
	err := changeReceiverCommand([]string{"f.go", "T:M"})
	assertExitCode(t, err, exitUsage)
	err = changeReceiverCommand([]string{"f.go", "T:M", "--pointer", "--value"})
	assertExitCode(t, err, exitUsage)
	err = changeReceiverCommand([]string{"f.go", "NotAMethod", "--pointer"})
	assertExitCode(t, err, exitUsage)
}

func TestChangeReceiverMissingMethod(t *testing.T) {
	t.Chdir(t.TempDir())
	path := writeTempGo(t, ".", "f.go", counterSrc)

	err := changeReceiverCommand([]string{path, "Counter:Reset", "--pointer"})
	assertExitCode(t, err, exitNotFound)
	if !strings.Contains(err.Error(), "Counter:Get") || !strings.Contains(err.Error(), "Counter:Inc") {
		t.Fatalf("error should list method candidates: %v", err)
	}
}

func TestChangeReceiverPreservesOtherMethods(t *testing.T) {
	t.Chdir(t.TempDir())
	path := writeTempGo(t, ".", "f.go",
		"package x\n\ntype T struct{ n int }\n\nfunc (t *T) Set(v int) { t.n = v }\n\nfunc (t *T) Read() int { return t.n }\n")

	if err := changeReceiverCommand([]string{path, "T:Set", "--value"}); err != nil {
		t.Fatalf("change-receiver: %v", err)
	}
	got := readFile(t, path)
	if !strings.Contains(got, "func (t T) Set(v int)") {
		t.Fatalf("Set should have value receiver:\n%s", got)
	}
	// Read should be unchanged.
	if !strings.Contains(got, "func (t *T) Read() int") {
		t.Fatalf("Read should still have pointer receiver:\n%s", got)
	}
}
