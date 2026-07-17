package doctor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// JournalEntry is one diagnose run's record in .gorefactor/doctor-history.jsonl
// (the failures-corpus pattern, plan decision 13). The journal is the data
// source for rule graduation (false-positive evidence) and the prevention-loop
// metric (new findings per edit trending down). Only new findings are stored;
// the baseline backlog is reconstructible from the baseline cache.
type JournalEntry struct {
	Time        time.Time         `json:"time"`
	BaseRef     string            `json:"baseRef"`
	BaseSHA     string            `json:"baseSHA,omitempty"`
	Scope       []string          `json:"scope,omitempty"`
	Substrates  []SubstrateStatus `json:"substrates"`
	NewCount    map[Severity]int  `json:"newCount"`
	FixedCount  map[Severity]int  `json:"fixedCount,omitempty"`
	NewFindings []JournalFinding  `json:"newFindings,omitempty"`
}

// JournalFinding is the compact per-finding journal record.
type JournalFinding struct {
	Fingerprint string   `json:"fingerprint"`
	Rule        string   `json:"rule"`
	Substrate   string   `json:"substrate"`
	File        string   `json:"file,omitempty"`
	Severity    Severity `json:"severity"`
}

const journalFileName = "doctor-history.jsonl"

// appendJournal appends one entry; the journal is a passive sensor and never
// gates, so failures here are returned for logging but must not fail a run.
func appendJournal(root string, entry JournalEntry) error {
	dir := filepath.Join(root, ".gorefactor")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create .gorefactor dir: %w", err)
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("encode journal entry: %w", err)
	}
	fh, err := os.OpenFile(filepath.Join(dir, journalFileName), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open journal: %w", err)
	}
	defer fh.Close()
	if _, err := fh.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("append journal: %w", err)
	}
	return nil
}

// journalEntryFor condenses a report into its journal record.
func journalEntryFor(r *Report) JournalEntry {
	entry := JournalEntry{
		Time:       time.Now().UTC(),
		BaseRef:    r.BaseRef,
		BaseSHA:    r.BaseSHA,
		Scope:      r.Scope,
		Substrates: r.Substrates,
		NewCount:   r.NewCount,
		FixedCount: r.FixedCount,
	}
	for _, f := range r.Findings {
		if !f.New {
			continue
		}
		entry.NewFindings = append(entry.NewFindings, JournalFinding{
			Fingerprint: f.Fingerprint,
			Rule:        f.Rule,
			Substrate:   f.Substrate,
			File:        f.File,
			Severity:    f.Severity,
		})
	}
	return entry
}
