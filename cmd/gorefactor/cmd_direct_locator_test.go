package main

import (
	"strings"
	"testing"
)

const methodLocatorSrc = `package main

type greeterRule struct{}

func (greeterRule) Name() string { return "greeter" }

func (r greeterRule) Run() int {
	return 1
}

func register() {
	addCommand(command{
		name:        "doctor",
		description: "Aggregate health gate: lint + build + test.",
	})
}

type command struct {
	name        string
	description string
}

func addCommand(c command) {}

func main() {}
`

// TestInsertAfterReceiverMethod locks in that before:/after:/inside: accept
// the Receiver:Method locator form used everywhere else. parseLocSpec used
// to stuff "Recv:Method" into FunctionName verbatim, so the lookup failed
// while the error's candidate list showed exactly that name.
func TestInsertAfterReceiverMethod(t *testing.T) {
	writeModule(t, map[string]string{"main.go": methodLocatorSrc})
	captureStdout(t, func() {
		if err := insertCommand([]string{"main.go", "after:greeterRule:Run", "func (r greeterRule) AutoFix() error { return nil }"}); err != nil {
			t.Fatalf("insert after:Receiver:Method: %v", err)
		}
	})
	src := readFile(t, "main.go")
	idx := strings.Index(src, "func (r greeterRule) Run() int")
	autofix := strings.Index(src, "func (r greeterRule) AutoFix() error")
	if autofix < 0 {
		t.Fatalf("inserted method missing:\n%s", src)
	}
	if autofix < idx {
		t.Errorf("AutoFix should be inserted after Run:\n%s", src)
	}
}

// TestEditFragmentFallsBackWithoutClobbering replays the incident that
// motivated exact statement matching: editing a string literal inside a
// composite-literal call must fall back to body-text replace and touch only
// the literal — the old substring matcher replaced the entire enclosing
// call statement with the fragment.
func TestEditFragmentFallsBackWithoutClobbering(t *testing.T) {
	writeModule(t, map[string]string{"main.go": methodLocatorSrc})
	captureStdout(t, func() {
		if err := editCommand([]string{"main.go", "register",
			`"Aggregate health gate: lint + build + test."`,
			`"Aggregate health gate: lint + golangci + build + test."`}); err != nil {
			t.Fatalf("edit: %v", err)
		}
	})
	src := readFile(t, "main.go")
	if !strings.Contains(src, "addCommand(command{") || !strings.Contains(src, `name:        "doctor",`) {
		t.Errorf("enclosing call must survive a fragment edit:\n%s", src)
	}
	if !strings.Contains(src, "lint + golangci + build + test") {
		t.Errorf("fragment replacement missing:\n%s", src)
	}
}
