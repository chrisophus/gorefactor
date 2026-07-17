package main

import (
	"strings"
	"testing"
)

func TestSplitByModeRoutesAdvisoryVsHard(t *testing.T) {
	orig := doctorGateMode
	defer func() { doctorGateMode = orig }()

	doctorGateMode = "advisory"
	blocking, advisory := splitByMode("[gosec/sec] a.go:3: boom")
	if blocking != "" || !strings.Contains(advisory, "boom") {
		t.Fatalf("advisory mode must not block: %q %q", blocking, advisory)
	}

	doctorGateMode = "hard"
	blocking, advisory = splitByMode("[gosec/sec] a.go:3: boom")
	if advisory != "" || !strings.Contains(blocking, "boom") {
		t.Fatalf("hard mode must block: %q %q", blocking, advisory)
	}

	if b, a := splitByMode(""); b != "" || a != "" {
		t.Fatalf("empty text is silent: %q %q", b, a)
	}
}

func TestPreflightDoctorGateOnlyHardMode(t *testing.T) {
	orig := doctorGateMode
	defer func() { doctorGateMode = orig }()
	doctorGateMode = "advisory"
	if err := preflightDoctorGate("."); err != nil {
		t.Fatalf("advisory mode never fails preflight: %v", err)
	}
	doctorGateMode = "off"
	if err := preflightDoctorGate("."); err != nil {
		t.Fatalf("off mode never fails preflight: %v", err)
	}
}
