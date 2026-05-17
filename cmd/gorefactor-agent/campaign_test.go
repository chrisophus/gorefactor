package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// newBigRepo makes a temp git module containing one oversized .go file
// (so enumerateFindings yields a file-size finding) plus a small clean
// one. Gate is green at baseline (compiles; no tests = pass).
func newBigRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	var big strings.Builder
	big.WriteString("package big\n\n")
	for i := 0; i < 360; i++ {
		fmt.Fprintf(&big, "func f%d() int { return %d }\n", i, i)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module big\n\ngo 1.21\n")
	write("big.go", big.String())
	write("small.go", "package big\n\nfunc Small() int { return 1 }\n")
	for _, args := range [][]string{
		{"init", "-q"}, {"config", "user.email", "t@e.com"},
		{"config", "user.name", "t"}, {"add", "-A"}, {"commit", "-q", "-m", "init"},
	} {
		c := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func TestCampaign_DetectsDelegatesAndPuntsCleanly(t *testing.T) {
	dir := newBigRepo(t)
	before, _ := os.ReadFile(filepath.Join(dir, "big.go"))

	// The junior punts every finding (split is hard); the campaign
	// must record it, leave the tree clean, and still succeed overall.
	mock := &mockToolProvider{
		script:     []chatMessage{asstCall("punt", `{"reason":"file split needs multi-op restructuring beyond a safe single pass"}`)},
		repeatLast: true,
	}

	var log bytes.Buffer
	err := RunCampaign(context.Background(), mock, Config{Dir: dir, Out: &log})
	if err != nil {
		t.Fatalf("campaign returned infra error: %v\n%s", err, log.String())
	}
	s := log.String()
	if !strings.Contains(s, "file-size") || !strings.Contains(s, "punted") {
		t.Fatalf("expected a detected file-size finding that punted:\n%s", s)
	}
	// metrics block present & well-formed
	a := strings.Index(s, "<<<CAMPAIGN_METRICS")
	b := strings.Index(s, "CAMPAIGN_METRICS>>>")
	if a < 0 || b < 0 {
		t.Fatalf("no CAMPAIGN_METRICS block:\n%s", s)
	}
	if !strings.Contains(s[a:b], `"frontier_tokens": 0`) {
		t.Fatalf("metrics must assert zero frontier tokens:\n%s", s[a:b])
	}
	after, _ := os.ReadFile(filepath.Join(dir, "big.go"))
	if string(before) != string(after) {
		t.Fatalf("punt must leave the oversized file untouched")
	}
}

func TestCampaign_NoFindingsIsClean(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module ok\n\ngo 1.21\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("package ok\n\nfunc A() {}\n"), 0o644)
	for _, args := range [][]string{
		{"init", "-q"}, {"config", "user.email", "t@e.com"},
		{"config", "user.name", "t"}, {"add", "-A"}, {"commit", "-q", "-m", "i"},
	} {
		exec.Command("git", append([]string{"-C", dir}, args...)...).Run()
	}
	mock := &mockToolProvider{} // must never be called
	var log bytes.Buffer
	if err := RunCampaign(context.Background(), mock, Config{Dir: dir, Out: &log}); err != nil {
		t.Fatalf("clean repo should yield nil: %v\n%s", err, log.String())
	}
	if mock.calls != 0 {
		t.Fatalf("model must not be called when there are no findings (calls=%d)", mock.calls)
	}
	if !strings.Contains(log.String(), "no deterministic findings") {
		t.Fatalf("expected clean message:\n%s", log.String())
	}
}
