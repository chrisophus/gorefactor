package main

import (
	"encoding/json"
	"strings"
	"testing"
)

const extractIfaceSrc = `package main

type Service struct{}

func (s *Service) Fetch(key string) (string, error) { return key, nil }
func (s *Service) Store(key, val string) error       { return nil }
func (s *Service) Close() error                      { return nil }
func (s *Service) private()                          {}
`

func TestExtractInterfaceAllMethods(t *testing.T) {
	writeModule(t, map[string]string{"main.go": extractIfaceSrc})
	if err := extractInterfaceCommand([]string{"main.go", "Service", "Storer"}); err != nil {
		t.Fatalf("extract-interface: %v", err)
	}
	src := readFile(t, "main.go")
	// Interface declaration present.
	if !strings.Contains(src, "type Storer interface {") {
		t.Fatalf("interface type not found:\n%s", src)
	}
	// All three exported methods present.
	for _, m := range []string{"Close()", "Fetch(", "Store("} {
		if !strings.Contains(src, m) {
			t.Fatalf("method %s not in interface:\n%s", m, src)
		}
	}
	// Unexported method must NOT be in the interface block.
	// Extract just the interface declaration.
	ifaceStart := strings.Index(src, "type Storer interface {")
	if ifaceStart == -1 {
		t.Fatalf("interface block not found:\n%s", src)
	}
	// Bound the check to just the interface body (between the opening { and its matching }).
	ifaceBody := src[ifaceStart:]
	if end := strings.Index(ifaceBody, "\n}"); end >= 0 {
		ifaceBody = ifaceBody[:end+2]
	}
	if strings.Contains(ifaceBody, "private") {
		t.Fatalf("unexported method should not appear in interface body:\n%s", ifaceBody)
	}
}

func TestExtractInterfaceWithMethodsFilter(t *testing.T) {
	writeModule(t, map[string]string{"main.go": extractIfaceSrc})
	if err := extractInterfaceCommand([]string{"main.go", "Service", "Fetcher", "--methods", "Fetch,Close"}); err != nil {
		t.Fatalf("extract-interface --methods: %v", err)
	}
	src := readFile(t, "main.go")
	ifaceStart := strings.Index(src, "type Fetcher interface {")
	if ifaceStart == -1 {
		t.Fatalf("interface not found:\n%s", src)
	}
	ifaceBlock := src[ifaceStart:]
	if !strings.Contains(ifaceBlock, "Fetch(") || !strings.Contains(ifaceBlock, "Close()") {
		t.Fatalf("expected Fetch and Close in interface block:\n%s", ifaceBlock)
	}
	if strings.Contains(ifaceBlock, "Store(") {
		t.Fatalf("Store should be excluded by --methods filter in interface block:\n%s", ifaceBlock)
	}
}

func TestExtractInterfaceUnknownMethodError(t *testing.T) {
	writeModule(t, map[string]string{"main.go": extractIfaceSrc})
	err := extractInterfaceCommand([]string{"main.go", "Service", "X", "--methods", "Fetch,NoSuch"})
	assertExitCode(t, err, exitNotFound)
	if !strings.Contains(err.Error(), "NoSuch") {
		t.Fatalf("error should name the missing method, got: %v", err)
	}
}

func TestExtractInterfaceDuplicateNameError(t *testing.T) {
	src := `package main

type Closer interface { Close() error }
type Service struct{}
func (s *Service) Close() error { return nil }
`
	writeModule(t, map[string]string{"main.go": src})
	err := extractInterfaceCommand([]string{"main.go", "Service", "Closer"})
	assertExitCode(t, err, exitNotFound)
	if !strings.Contains(err.Error(), "already declared") {
		t.Fatalf("expected 'already declared' error, got: %v", err)
	}
}

func TestExtractInterfaceMissingTypeError(t *testing.T) {
	writeModule(t, map[string]string{"main.go": extractIfaceSrc})
	err := extractInterfaceCommand([]string{"main.go", "NoSuchType", "MyIface"})
	assertExitCode(t, err, exitNotFound)
}

func TestExtractInterfaceDryRunDoesNotWrite(t *testing.T) {
	writeModule(t, map[string]string{"main.go": extractIfaceSrc})
	before := readFile(t, "main.go")
	if err := extractInterfaceCommand([]string{"main.go", "Service", "Storer", "--dry-run"}); err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if readFile(t, "main.go") != before {
		t.Fatal("--dry-run must not write the file")
	}
}

func TestExtractInterfaceJSONOutput(t *testing.T) {
	writeModule(t, map[string]string{"main.go": extractIfaceSrc})
	out := captureStdout(t, func() {
		if err := extractInterfaceCommand([]string{"main.go", "Service", "Storer", "--json"}); err != nil {
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
	if res.UndoToken == "" {
		t.Fatal("undo token must be set on success")
	}
	if !strings.Contains(res.Detail, "find-implementations") {
		t.Fatalf("detail should carry find-implementations hint, got: %s", res.Detail)
	}
}

func TestExtractInterfaceNoExportedMethods(t *testing.T) {
	src := `package main

type Hidden struct{}

func (h *Hidden) foo() {}
`
	writeModule(t, map[string]string{"main.go": src})
	err := extractInterfaceCommand([]string{"main.go", "Hidden", "HiddenIface"})
	assertExitCode(t, err, exitNotFound)
}
