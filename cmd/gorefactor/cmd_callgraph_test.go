package main

import (
	"strings"
	"testing"
)

const callgraphTestSrc = `package x

func Top() {
	Middle()
}

func Middle() {
	Leaf()
	Loop()
}

func Leaf() {}

func Loop() {
	Middle()
}

type Svc struct{}

func (s *Svc) Handle() {
	Top()
}
`

func TestCallgraphCallees(t *testing.T) {
	t.Chdir(t.TempDir())
	writeTempGo(t, ".", "x.go", callgraphTestSrc)

	out := captureStdout(t, func() {
		if err := callgraphCommand([]string{"Top", "--depth", "3"}); err != nil {
			t.Errorf("callgraph: %v", err)
		}
	})
	for _, want := range []string{
		"Top  x.go:3",
		"  Middle  x.go:7",
		"    Leaf  x.go:12",
		"    Loop  x.go:14",
		"      Middle  x.go:7 [cycle]",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("callees output missing %q:\n%s", want, out)
		}
	}
}

func TestCallgraphCallers(t *testing.T) {
	t.Chdir(t.TempDir())
	writeTempGo(t, ".", "x.go", callgraphTestSrc)

	out := captureStdout(t, func() {
		if err := callgraphCommand([]string{"Leaf", "--callers", "--depth", "3"}); err != nil {
			t.Errorf("callgraph --callers: %v", err)
		}
	})
	for _, want := range []string{
		"Leaf  x.go:12",
		"  Middle  x.go:7",
		"    Top  x.go:3",
		"      Svc:Handle  x.go:20",
		"    Loop  x.go:14",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("callers output missing %q:\n%s", want, out)
		}
	}
}

func TestCallgraphJSONAndDepthLimit(t *testing.T) {
	t.Chdir(t.TempDir())
	writeTempGo(t, ".", "x.go", callgraphTestSrc)

	out := captureStdout(t, func() {
		if err := callgraphCommand([]string{"Top", "--depth", "1", "--json"}); err != nil {
			t.Errorf("callgraph --json: %v", err)
		}
	})
	var res struct {
		Target    string  `json:"target"`
		Direction string  `json:"direction"`
		Depth     int     `json:"depth"`
		Tree      *cgNode `json:"tree"`
	}
	decodeEnvelope(t, out, &res)
	if res.Target != "Top" || res.Direction != "callees" || res.Depth != 1 {
		t.Fatalf("unexpected payload: %+v", res)
	}
	if len(res.Tree.Children) != 1 || res.Tree.Children[0].Name != "Middle" {
		t.Fatalf("unexpected tree: %+v", res.Tree)
	}
	// depth 1 must not expand Middle's callees
	if len(res.Tree.Children[0].Children) != 0 {
		t.Fatalf("depth 1 should not expand grandchildren: %+v", res.Tree.Children[0])
	}
}

func TestCallgraphMethodLocatorAndNotFound(t *testing.T) {
	t.Chdir(t.TempDir())
	writeTempGo(t, ".", "x.go", callgraphTestSrc)

	out := captureStdout(t, func() {
		if err := callgraphCommand([]string{"Svc:Handle"}); err != nil {
			t.Errorf("method locator: %v", err)
		}
	})
	if !strings.Contains(out, "Svc:Handle") || !strings.Contains(out, "  Top  x.go:3") {
		t.Fatalf("method callgraph wrong:\n%s", out)
	}

	err := callgraphCommand([]string{"Nope"})
	assertExitCode(t, err, exitNotFound)
	if !strings.Contains(err.Error(), "Top") {
		t.Fatalf("not-found error should list candidates: %v", err)
	}
}
