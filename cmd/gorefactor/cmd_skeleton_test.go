package main

import (
	"strings"
	"testing"
)

const skeletonTestSrc = `package x

import "fmt"

// Limit is the max.
const Limit = 10

// Widget is a thing.
type Widget struct {
	Name string
}

// Greet says hello.
func (w *Widget) Greet(prefix string) string {
	msg := prefix + w.Name
	fmt.Println(msg)
	return msg
}

func tiny() {}
`

func TestSkeletonElidesBodies(t *testing.T) {
	t.Chdir(t.TempDir())
	path := writeTempGo(t, ".", "w.go", skeletonTestSrc)

	out := captureStdout(t, func() {
		if err := skeletonCommand([]string{path}); err != nil {
			t.Errorf("skeleton: %v", err)
		}
	})

	for _, want := range []string{
		"// Limit is the max.",
		"const Limit = 10",
		"type Widget struct {",
		"// Greet says hello.",
		"func (w *Widget) Greet(prefix string) string { /* 3 lines */ }",
		"func tiny() { … }",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("skeleton output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "fmt.Println(msg)") {
		t.Errorf("skeleton must elide function bodies:\n%s", out)
	}
}

func TestSkeletonJSONOutline(t *testing.T) {
	t.Chdir(t.TempDir())
	path := writeTempGo(t, ".", "w.go", skeletonTestSrc)

	out := captureStdout(t, func() {
		if err := skeletonCommand([]string{path, "--json"}); err != nil {
			t.Errorf("skeleton --json: %v", err)
		}
	})
	var outline skeletonOutline
	decodeEnvelope(t, out, &outline)
	if outline.Package != "x" {
		t.Fatalf("package = %q", outline.Package)
	}
	byName := map[string]skeletonDecl{}
	for _, d := range outline.Decls {
		byName[d.Name] = d
	}
	if d := byName["Greet"]; d.Kind != "method" || d.Receiver != "*Widget" || d.BodyLines != 3 {
		t.Fatalf("Greet decl: %+v", d)
	}
	if !strings.Contains(byName["Greet"].Signature, "Greet(prefix string) string") {
		t.Fatalf("Greet signature: %q", byName["Greet"].Signature)
	}
	if byName["Greet"].Doc != "Greet says hello." {
		t.Fatalf("Greet doc: %q", byName["Greet"].Doc)
	}
	if d := byName["Widget"]; d.Kind != "type" {
		t.Fatalf("Widget decl: %+v", d)
	}
	if d := byName["Limit"]; d.Kind != "const" {
		t.Fatalf("Limit decl: %+v", d)
	}
}

func TestSkeletonExitCodes(t *testing.T) {
	t.Chdir(t.TempDir())
	err := skeletonCommand([]string{"missing.go"})
	assertExitCode(t, err, exitNotFound)

	bad := writeTempGo(t, ".", "bad.go", "this is not go\\n")
	err = skeletonCommand([]string{bad})
	assertExitCode(t, err, exitParseError)
}
