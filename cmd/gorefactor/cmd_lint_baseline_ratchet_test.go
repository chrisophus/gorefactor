package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestBaselineGrowth(t *testing.T) {
	old := &baselineFile{Issues: []baselineEntry{
		{Fingerprint: "a", File: "a.go", Rule: "long-function", Count: 2},
		{Fingerprint: "b", File: "b.go", Rule: "complexity", Count: 1},
	}}
	shrunk := &baselineFile{Issues: []baselineEntry{
		{Fingerprint: "a", File: "a.go", Rule: "long-function", Count: 1},
	}}
	if g := baselineGrowth(old, shrunk); len(g) != 0 {
		t.Fatalf("shrinking must pass: %v", g)
	}
	grown := &baselineFile{Issues: []baselineEntry{
		{Fingerprint: "a", File: "a.go", Rule: "long-function", Count: 3},
		{Fingerprint: "b", File: "b.go", Rule: "complexity", Count: 1},
		{Fingerprint: "c", File: "c.go", Rule: "dead-code", Count: 1},
	}}
	g := baselineGrowth(old, grown)
	if len(g) != 2 {
		t.Fatalf("growth = %v, want new entry c + count increase a", g)
	}
	joined := strings.Join(g, "\n")
	if !strings.Contains(joined, "c.go") || !strings.Contains(joined, "2 -> 3") {
		t.Fatalf("growth messages = %v", g)
	}
}

func TestBaselineRatchetCommand_FailsOnGrowth(t *testing.T) {
	initRatchetRepo(t, ratchetBaseV1)
	grown := `{"version": 1, "issues": [
	  {"fingerprint": "a", "file": "a.go", "rule": "long-function", "count": 2},
	  {"fingerprint": "z", "file": "z.go", "rule": "dead-code", "count": 1}
	]}`
	if err := os.WriteFile(defaultBaselinePath, []byte(grown), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := baselineRatchetCommand(defaultBaselinePath, "HEAD"); err == nil {
		t.Fatal("grown baseline must fail the ratchet")
	}
}

const ratchetBaseV1 = `{"version": 1, "issues": [
  {"fingerprint": "a", "file": "a.go", "rule": "long-function", "count": 2}
]}`

func TestBaselineRatchetCommand_PassesOnShrinkAndHold(t *testing.T) {
	initRatchetRepo(t, ratchetBaseV1)
	if err := baselineRatchetCommand(defaultBaselinePath, "HEAD"); err != nil {
		t.Fatalf("unchanged baseline must pass: %v", err)
	}
	shrunk := `{"version": 1, "issues": []}`
	if err := os.WriteFile(defaultBaselinePath, []byte(shrunk), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := baselineRatchetCommand(defaultBaselinePath, "HEAD"); err != nil {
		t.Fatalf("shrunken baseline must pass: %v", err)
	}
}

func TestBaselineRatchetCommand_IntroductionAndDeletion(t *testing.T) {
	initRatchetRepo(t, "")
	if err := os.WriteFile(defaultBaselinePath, []byte(ratchetBaseV1), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := baselineRatchetCommand(defaultBaselinePath, "HEAD"); err != nil {
		t.Fatalf("introducing a baseline must pass: %v", err)
	}
	if err := os.Remove(defaultBaselinePath); err != nil {
		t.Fatal(err)
	}
	if err := baselineRatchetCommand(defaultBaselinePath, "HEAD"); err != nil {
		t.Fatalf("no baseline anywhere: nothing to check: %v", err)
	}
	// Commit the baseline, then delete it: disabling the ratchet must fail.
	if err := os.WriteFile(defaultBaselinePath, []byte(ratchetBaseV1), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-q", "-m", "add baseline"}} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	if err := os.Remove(defaultBaselinePath); err != nil {
		t.Fatal(err)
	}
	err := baselineRatchetCommand(defaultBaselinePath, "HEAD")
	if err == nil || !strings.Contains(err.Error(), "deleted") {
		t.Fatalf("deleting a committed baseline must fail, got %v", err)
	}

}

// initRatchetRepo creates a temp git repo with a committed baseline and
// chdirs into it for the duration of the test.
func initRatchetRepo(t *testing.T, committed string) {
	t.Helper()
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "test@test"},
		{"config", "user.name", "test"},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	if committed != "" {
		if err := os.WriteFile(defaultBaselinePath, []byte(committed), 0o644); err != nil {
			t.Fatal(err)
		}
	} else {
		if err := os.WriteFile("README.md", []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-q", "-m", "base"}} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
}
