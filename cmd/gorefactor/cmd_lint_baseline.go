package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/chrisophus/gorefactor/doctor"
)

// defaultBaselinePath is where --write-baseline / --baseline read and write
// when no --baseline-file is given. It lives at the repo root (not under the
// gitignored .gorefactor/ dir) precisely so it can be committed: a ratchet is
// only useful if CI shares the same baseline the developer recorded.
const defaultBaselinePath = ".gorefactor-lint-baseline.json"

const baselineFormatVersion = 1

// baselineFile is the on-disk ratchet snapshot. It records, per fingerprint,
// how many matching issues existed when the baseline was written. The array is
// sorted by fingerprint so the committed file diffs cleanly as issues are
// added or paid down.
type baselineFile struct {
	Version int             `json:"version"`
	Issues  []baselineEntry `json:"issues"`
}

type baselineEntry struct {
	Fingerprint string `json:"fingerprint"`
	File        string `json:"file"`
	Rule        string `json:"rule"`
	Count       int    `json:"count"`
}

// issueFingerprint identifies an issue independently of the exact line it sits
// on, so a finding that merely shifts when unrelated code is added above it is
// still recognised as the same pre-existing issue. It combines the file, the
// rule, and the message with all digit runs collapsed (line numbers, sizes,
// impact scores) — the stable parts (symbol names, file paths, phrasing)
// survive, the volatile numbers do not.
func issueFingerprint(iss lintIssue) string {
	return iss.File + "\x00" + iss.Rule + "\x00" + normalizeLintMessage(iss.Message)
}

// normalizeLintMessage replaces every maximal run of ASCII digits with a
// single '#', turning "is 80 lines (threshold 75, line 98)" into
// "is # lines (threshold #, line #)". This is what makes the fingerprint
// resilient to line drift and small size changes in an otherwise-identical
// finding.
func normalizeLintMessage(msg string) string {
	return doctor.NormalizeMessage(msg)

}

// writeBaseline records the full current issue set (every severity, so nothing
// is lost from the ratchet) as a sorted baseline snapshot at path.
func writeBaseline(path string, issues []lintIssue) error {
	// Keep one representative entry per fingerprint for the File/Rule columns,
	// plus its occurrence count.
	type agg struct {
		file, rule string
		count      int
	}
	byFP := map[string]*agg{}
	for _, iss := range issues {
		fp := issueFingerprint(iss)
		a, ok := byFP[fp]
		if !ok {
			a = &agg{file: iss.File, rule: iss.Rule}
			byFP[fp] = a
		}
		a.count++
	}
	entries := make([]baselineEntry, 0, len(byFP))
	for fp, a := range byFP {
		entries = append(entries, baselineEntry{
			Fingerprint: fp, File: a.file, Rule: a.rule, Count: a.count,
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Fingerprint < entries[j].Fingerprint
	})

	bf := baselineFile{Version: baselineFormatVersion, Issues: entries}
	data, err := json.MarshalIndent(bf, "", "  ")
	if err != nil {
		return fmt.Errorf("encode baseline: %w", err)
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create baseline dir: %w", err)
		}
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write baseline: %w", err)
	}
	return nil
}

// loadBaseline reads a baseline snapshot into a fingerprint -> count map.
func loadBaseline(path string) (map[string]int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no lint baseline at %s — run `gorefactor lint --write-baseline` first", path)
		}
		return nil, fmt.Errorf("read baseline: %w", err)
	}
	var bf baselineFile
	if err := json.Unmarshal(data, &bf); err != nil {
		return nil, fmt.Errorf("parse baseline %s: %w", path, err)
	}
	if bf.Version != baselineFormatVersion {
		return nil, fmt.Errorf("baseline %s has version %d, expected %d — rewrite it with --write-baseline",
			path, bf.Version, baselineFormatVersion)
	}
	counts := make(map[string]int, len(bf.Issues))
	for _, e := range bf.Issues {
		counts[e.Fingerprint] += e.Count
	}
	return counts, nil
}

// filterAgainstBaseline returns only the issues that are new or worsened
// relative to the baseline: for each fingerprint, the first baseline[fp]
// occurrences are suppressed and any beyond that count are surfaced. Issues are
// consumed in their incoming (already sorted) order, so the surfaced ones are
// deterministic.
func filterAgainstBaseline(issues []lintIssue, baseline map[string]int) []lintIssue {
	remaining := make(map[string]int, len(baseline))
	for fp, n := range baseline {
		remaining[fp] = n
	}
	out := make([]lintIssue, 0, len(issues))
	for _, iss := range issues {
		fp := issueFingerprint(iss)
		if remaining[fp] > 0 {
			remaining[fp]--
			continue
		}
		out = append(out, iss)
	}
	return out
}

// baselineFilePath resolves the baseline path for this run: an explicit
// --baseline-file wins, otherwise config baseline.file, otherwise the default
// at the lint root.
func (opts lintOptions) baselineFilePath() string {
	if opts.baselineFile != "" {
		if filepath.IsAbs(opts.baselineFile) {
			return opts.baselineFile
		}
		return filepath.Join(opts.root, opts.baselineFile)
	}
	file := defaultBaselinePath
	if opts.cfg != nil {
		file = opts.cfg.BaselineFile()
	}
	return filepath.Join(opts.root, file)
}
