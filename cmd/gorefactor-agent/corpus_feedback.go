package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Phase 6 (continued): close the mistake-cannot-recur loop by feeding the
// failure corpus back into the agent's own system prompt. corpus.go writes
// .gorefactor/failures.jsonl as a passive sensor; here we read it back,
// aggregate the recurring rejections, and render a compact "known failure
// modes" block so the model sees — before it acts — which op shapes this repo
// has already rejected. Input tokens dominate agentic cost, so the block is
// hard-bounded (a handful of lines) and empty on a cold repo.

// corpusMaxScan caps how many trailing corpus lines are parsed. The corpus is
// append-only and can grow without bound across sessions; only recent failures
// are relevant, and the read happens once per run at prompt assembly.
const corpusMaxScan = 2000

// corpusFeedbackMaxTools bounds how many distinct rejected tools are surfaced,
// keeping the prompt block short regardless of corpus size.
const corpusFeedbackMaxTools = 5

// readFailureCorpus parses up to the last corpusMaxScan entries of the corpus
// under dir. Best-effort: a missing or malformed corpus yields no entries and
// never errors, because it must never affect a run.
func readFailureCorpus(dir string, maxScan int) []failureEntry {
	f, err := os.Open(filepath.Join(dir, corpusRelPath))
	if err != nil {
		return nil
	}
	defer f.Close()

	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		lines = append(lines, line)
		if maxScan > 0 && len(lines) > maxScan {
			lines = lines[1:] // keep only the trailing window
		}
	}
	entries := make([]failureEntry, 0, len(lines))
	for _, line := range lines {
		var e failureEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		entries = append(entries, e)
	}
	return entries
}

// toolFailureStat aggregates one tool's rejections: total count plus the most
// common (digit-normalized) reason with a raw example of it.
type toolFailureStat struct {
	tool        string
	count       int
	topReason   string // raw example of the most common normalized reason
	topReasonN  int
	reasonTally map[string]int    // normalized reason -> count
	reasonEx    map[string]string // normalized reason -> raw example
}

// failureCorpusSection renders the "known failure modes" prompt block from the
// corpus under dir, or "" when there is nothing worth surfacing.
func failureCorpusSection(dir string) string {
	entries := readFailureCorpus(dir, corpusMaxScan)
	if len(entries) == 0 {
		return ""
	}
	ranked, kindCounts := aggregateFailures(entries)
	return renderFailureSection(ranked, kindCounts)
}

// aggregateFailures folds corpus entries into per-tool rejection stats (ranked
// most-rejected first) and a per-kind count map.
func aggregateFailures(entries []failureEntry) (ranked []*toolFailureStat, kindCounts map[string]int) {
	stats := map[string]*toolFailureStat{}
	kindCounts = map[string]int{}
	for _, e := range entries {
		kindCounts[e.Kind]++
		if e.Kind != failRejectedOp || e.Tool == "" {
			continue
		}
		s := stats[e.Tool]
		if s == nil {
			s = &toolFailureStat{tool: e.Tool, reasonTally: map[string]int{}, reasonEx: map[string]string{}}
			stats[e.Tool] = s
		}
		s.count++
		if norm := normalizeCorpusReason(e.Reason); norm != "" {
			s.reasonTally[norm]++
			if _, ok := s.reasonEx[norm]; !ok {
				s.reasonEx[norm] = e.Reason
			}
		}
	}
	for _, s := range stats {
		for norm, n := range s.reasonTally {
			if n > s.topReasonN {
				s.topReasonN = n
				s.topReason = s.reasonEx[norm]
			}
		}
		ranked = append(ranked, s)
	}
	// Most-rejected tools first; break ties by name for determinism.
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].count != ranked[j].count {
			return ranked[i].count > ranked[j].count
		}
		return ranked[i].tool < ranked[j].tool
	})
	return ranked, kindCounts
}

// renderFailureSection formats the ranked tool stats and kind counts into the
// prompt block, or "" when there is nothing worth surfacing.
func renderFailureSection(ranked []*toolFailureStat, kindCounts map[string]int) string {
	var b strings.Builder
	b.WriteString("\n\nKNOWN FAILURE MODES (this repo's failure corpus — do NOT repeat these; " +
		"when a shape below is unavoidable, run a sense/analysis tool first to confirm the target):\n")
	shown := 0
	for _, s := range ranked {
		if shown >= corpusFeedbackMaxTools {
			break
		}
		line := fmt.Sprintf("- %s rejected %d×", s.tool, s.count)
		if s.topReason != "" {
			line += fmt.Sprintf(": e.g. %q", trim(oneLine(s.topReason), 140))
		}
		b.WriteString(line + "\n")
		shown++
	}
	if g := kindCounts[failCapabilityGap]; g > 0 {
		b.WriteString(fmt.Sprintf("- %d capability-gap punt(s): a needed gorefactor command was missing — "+
			"prefer an available op or report the gap rather than forcing a bad fit\n", g))
	}
	if bh := kindCounts[failBudgetHit]; bh > 0 {
		b.WriteString(fmt.Sprintf("- %d prior budget exhaustion(s): keep plans minimal and act early\n", bh))
	}
	if shown == 0 && kindCounts[failCapabilityGap] == 0 && kindCounts[failBudgetHit] == 0 {
		return ""
	}
	return b.String()
}

// normalizeCorpusReason collapses digit runs to '#' so reasons differing only
// in line numbers / counts group together, and lowercases for stable tallying.
func normalizeCorpusReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return ""
	}
	var sb strings.Builder
	inDigits := false
	for i := 0; i < len(reason); i++ {
		c := reason[i]
		if c >= '0' && c <= '9' {
			if !inDigits {
				sb.WriteByte('#')
				inDigits = true
			}
			continue
		}
		inDigits = false
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		sb.WriteByte(c)
	}
	return strings.TrimSpace(sb.String())
}

// oneLine flattens whitespace/newlines so a multi-line rejection reason renders
// as a single prompt bullet.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
