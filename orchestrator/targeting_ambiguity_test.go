package orchestrator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeAmbiguityFixture(t *testing.T) string {
	t.Helper()
	testFile := filepath.Join(t.TempDir(), "ambiguous.go")
	code := `package main

type Server struct{}

type Client struct{}

func Process() {
	println("free function")
}

func (s *Server) Process() {
	println("server method")
}

func (c Client) Process() {
	println("client method")
}
`
	if err := os.WriteFile(testFile, []byte(code), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return testFile
}

func TestFindTargetBySemantics_AmbiguousTie(t *testing.T) {
	orch := NewOrchestrator()
	testFile := writeAmbiguityFixture(t)

	// Three declarations named Process tie without a receiver constraint.
	_, err := orch.findTargetBySemantics(&TargetSpecification{
		FunctionName: "Process",
		MethodName:   "Process",
	}, testFile)
	if err == nil {
		t.Fatal("expected ambiguity error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "ambiguous target") {
		t.Fatalf("expected ambiguity error, got: %v", err)
	}
	for _, want := range []string{"Server:Process", "Client:Process", testFile + ":7", testFile + ":11", testFile + ":15", "receiverType"} {
		if !strings.Contains(msg, want) {
			t.Errorf("ambiguity error missing %q:\n%s", want, msg)
		}
	}
}

func TestFindTargetBySemantics_ReceiverTypeDisambiguates(t *testing.T) {
	orch := NewOrchestrator()
	testFile := writeAmbiguityFixture(t)

	cases := []struct {
		name     string
		receiver string
		wantLine int
		wantRecv string
	}{
		{"pointer receiver", "Server", 11, "Server"},
		{"pointer receiver with star", "*Server", 11, "Server"},
		{"value receiver", "Client", 15, "Client"},
		{"plain function via ReceiverNone", ReceiverNone, 7, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			loc, err := orch.findTargetBySemantics(&TargetSpecification{
				FunctionName: "Process",
				MethodName:   "Process",
				ReceiverType: tc.receiver,
			}, testFile)
			if err != nil {
				t.Fatalf("findTargetBySemantics failed: %v", err)
			}
			if loc.StartLine != tc.wantLine {
				t.Errorf("StartLine = %d, want %d", loc.StartLine, tc.wantLine)
			}
			if loc.Method != tc.wantRecv {
				t.Errorf("Method = %q, want %q", loc.Method, tc.wantRecv)
			}
		})
	}
}

func TestFindTargetBySemantics_UniqueMatchStillWorks(t *testing.T) {
	orch := NewOrchestrator()
	testFile := filepath.Join(t.TempDir(), "unique.go")
	code := `package main

func Alpha() {}

func Beta() {}
`
	if err := os.WriteFile(testFile, []byte(code), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	loc, err := orch.findTargetBySemantics(&TargetSpecification{FunctionName: "Beta"}, testFile)
	if err != nil {
		t.Fatalf("findTargetBySemantics failed: %v", err)
	}
	if loc.Function != "Beta" || loc.StartLine != 5 {
		t.Errorf("got %+v, want Beta at line 5", loc)
	}
}

func TestFindTargetBySemantics_ReceiverMismatchNotFound(t *testing.T) {
	orch := NewOrchestrator()
	testFile := writeAmbiguityFixture(t)

	_, err := orch.findTargetBySemantics(&TargetSpecification{
		MethodName:   "Process",
		ReceiverType: "Widget",
	}, testFile)
	if err == nil {
		t.Fatal("expected no-match error for wrong receiver, got nil")
	}
	if !strings.Contains(err.Error(), "no suitable target") {
		t.Errorf("expected no-suitable-target error, got: %v", err)
	}
}
