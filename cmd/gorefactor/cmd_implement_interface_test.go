package main

import (
	"encoding/json"
	"strings"
	"testing"
)

const implIfaceSrc = `package main

type Writer interface {
	Write(p []byte) (n int, err error)
	Close() error
}

type PartialWriter struct{}

func (p *PartialWriter) Write(data []byte) (int, error) {
	return len(data), nil
}
`

func TestImplementInterfaceAddsStubs(t *testing.T) {
	writeModule(t, map[string]string{"main.go": implIfaceSrc})
	if err := implementInterfaceCommand([]string{"main.go", "PartialWriter", "Writer"}); err != nil {
		t.Fatalf("implement-interface: %v", err)
	}
	src := readFile(t, "main.go")
	if !strings.Contains(src, "func (p *PartialWriter) Close()") {
		t.Fatalf("missing Close stub:\n%s", src)
	}
	if strings.Contains(src, "func (p *PartialWriter) Write(") &&
		strings.Count(src, "func (p *PartialWriter) Write(") > 1 {
		t.Fatalf("Write should not be duplicated:\n%s", src)
	}
	if !strings.Contains(src, `panic("unimplemented")`) {
		t.Fatalf("stub should contain panic(\"unimplemented\"):\n%s", src)
	}
}

func TestImplementInterfaceAlreadyImplementedIsNoOp(t *testing.T) {
	src := `package main

type Doer interface {
	Do() error
}

type MyDoer struct{}

func (m *MyDoer) Do() error { return nil }
`
	writeModule(t, map[string]string{"main.go": src})
	before := readFile(t, "main.go")
	out := captureStdout(t, func() {
		if err := implementInterfaceCommand([]string{"main.go", "MyDoer", "Doer"}); err != nil {
			t.Errorf("implement-interface on already-satisfied: %v", err)
		}
	})
	if readFile(t, "main.go") != before {
		t.Fatal("already-implements should not modify the file")
	}
	if !strings.Contains(out, "already implements") {
		t.Fatalf("should report already implements, got: %s", out)
	}
}

func TestImplementInterfaceMissingTypeError(t *testing.T) {
	writeModule(t, map[string]string{"main.go": `package main

type MyIface interface { X() }
`})
	err := implementInterfaceCommand([]string{"main.go", "NoSuchType", "MyIface"})
	assertExitCode(t, err, exitNotFound)
}

func TestImplementInterfaceMissingInterfaceError(t *testing.T) {
	writeModule(t, map[string]string{"main.go": `package main

type MyType struct{}
`})
	err := implementInterfaceCommand([]string{"main.go", "MyType", "NoSuchIface"})
	assertExitCode(t, err, exitNotFound)
}

func TestImplementInterfaceDryRunDoesNotWrite(t *testing.T) {
	writeModule(t, map[string]string{"main.go": implIfaceSrc})
	before := readFile(t, "main.go")
	out := captureStdout(t, func() {
		if err := implementInterfaceCommand([]string{"main.go", "PartialWriter", "Writer", "--dry-run"}); err != nil {
			t.Errorf("dry-run: %v", err)
		}
	})
	if readFile(t, "main.go") != before {
		t.Fatal("--dry-run must not modify the file")
	}
	if !strings.Contains(out, "dry-run") {
		t.Fatalf("dry-run should say dry-run, got:\n%s", out)
	}
}

func TestImplementInterfaceJSONOutput(t *testing.T) {
	writeModule(t, map[string]string{"main.go": implIfaceSrc})
	out := captureStdout(t, func() {
		if err := implementInterfaceCommand([]string{"main.go", "PartialWriter", "Writer", "--json"}); err != nil {
			t.Errorf("--json: %v", err)
		}
	})
	var res mutationResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if !res.Success {
		t.Fatalf("expected success=true, got: %+v", res)
	}
	if res.UndoToken == "" {
		t.Fatal("success result must carry an undoToken")
	}
}

func TestImplementInterfaceJSONErrorOutput(t *testing.T) {
	writeModule(t, map[string]string{"main.go": `package main

type X struct{}
`})
	var jerr error
	out := captureStdout(t, func() {
		jerr = implementInterfaceCommand([]string{"main.go", "X", "NonExistentIface", "--json"})
	})
	assertExitCode(t, jerr, exitNotFound)
	var res mutationResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("error output not valid JSON: %v\n%s", err, out)
	}
	if res.Success || res.Error == "" {
		t.Fatalf("expected success=false with error message, got: %+v", res)
	}
}
