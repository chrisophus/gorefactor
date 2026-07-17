package main

import (
	"strings"
	"testing"

	"github.com/chrisophus/gorefactor/doctor"
)

func TestIntentCommandRoundTrip(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := intentCommand([]string{"api-change", "analyzer", "widening for feature X"}); err != nil {
		t.Fatal(err)
	}
	intents, err := doctor.LoadIntents(".")
	if err != nil || len(intents) != 1 {
		t.Fatalf("want 1 recorded intent: %v (%v)", intents, err)
	}
	if intents[0].Scope != "analyzer" {
		t.Fatalf("scope not recorded: %+v", intents[0])
	}
	out := captureStdout(t, func() {
		if err := intentCommand([]string{"--list"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "analyzer") || !strings.Contains(out, "widening for feature X") {
		t.Fatalf("--list should show the intent: %s", out)
	}
	if err := intentCommand([]string{"--clear"}); err != nil {
		t.Fatal(err)
	}
	if intents, _ := doctor.LoadIntents("."); len(intents) != 0 {
		t.Fatalf("--clear should remove intents: %v", intents)
	}
}

func TestIntentCommandRejectsMissingArgs(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := intentCommand([]string{"api-change", "analyzer"}); err == nil {
		t.Fatal("intent without a reason must be rejected")
	}
}

func TestParseDoctorArgsReportMode(t *testing.T) {
	opts, err := parseDoctorArgs([]string{"--report", "--base", "main", "--json", "--scoped", "subdir"})
	if err != nil {
		t.Fatal(err)
	}
	if !opts.report || opts.baseRef != "main" || !opts.jsonOut || !opts.scoped || opts.root != "subdir" {
		t.Fatalf("parse wrong: %+v", opts)
	}
	if _, err := parseDoctorArgs([]string{"--base"}); err == nil {
		t.Fatal("--base without a ref must error")
	}
}
