package main

import (
	"testing"

	"github.com/chrisophus/gorefactor/doctor"
)

// TestScoredSubstratesSkipped pins the score-honesty guard: a substrate that
// did not run must be surfaced (so a partial run never reads as a healthy
// score), and the baseline ratchet must not (it emits no scored findings).
func TestScoredSubstratesSkipped(t *testing.T) {
	subs := []doctor.SubstrateStatus{
		{Name: "structural", State: doctor.SubstrateRan},
		{Name: "deadcode", State: doctor.SubstrateSkipped},
		{Name: "govulncheck", State: doctor.SubstrateFailed},
		{Name: "baseline", State: doctor.SubstrateSkipped}, // excluded: not a finding source
	}
	got := scoredSubstratesSkipped(subs)
	want := []string{"deadcode", "govulncheck"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("index %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestScoredSubstratesSkipped_AllRan(t *testing.T) {
	subs := []doctor.SubstrateStatus{
		{Name: "structural", State: doctor.SubstrateRan},
		{Name: "deadcode", State: doctor.SubstrateRan},
	}
	if got := scoredSubstratesSkipped(subs); len(got) != 0 {
		t.Errorf("all-ran should skip none, got %v", got)
	}
}
