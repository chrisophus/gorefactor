package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

// TestDocDrift_IterationDefaultsMatch pins the iteration-default claims in
// CLAUDE.md to the constants that implement them, in the same spirit as
// cmd/gorefactor's TestDocDrift_RuleCountMatches: every count stated in prose
// must be derivable from the code. CLAUDE.md states the agentic default in two
// places ("up to N iterations by default" and "N for agentic") and the
// single-shot default once ("N for single-shot").
func TestDocDrift_IterationDefaultsMatch(t *testing.T) {
	doc := readAgentClaudeMD(t)
	checks := []struct {
		pattern string
		want    int
	}{
		{`up to (\d+) iterations by default`, maxToolCalls},
		{`(\d+) for agentic`, maxToolCalls},
		{`(\d+) for single-shot`, singleShotMaxIter},
	}
	for _, c := range checks {
		re := regexp.MustCompile(c.pattern)
		matches := re.FindAllStringSubmatch(doc, -1)
		if len(matches) == 0 {
			t.Errorf("CLAUDE.md no longer matches %q; update this test's pattern", c.pattern)
			continue
		}
		for _, m := range matches {
			if m[1] != strconv.Itoa(c.want) {
				t.Errorf("CLAUDE.md claims %q but the code default is %d; update the doc", m[0], c.want)
			}
		}
	}
}

// readAgentClaudeMD loads the repo-root CLAUDE.md relative to this test's
// package directory (cmd/gorefactor-agent -> ../../CLAUDE.md).
func readAgentClaudeMD(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}
	return string(b)
}
