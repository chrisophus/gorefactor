package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/chrisophus/gorefactor/orchestrator"
)

// cmd_adherence.go: the harness self-audit sensor (Tier-2 F0). It answers
// "did edits to Go code go THROUGH gorefactor, or around it?" — the repo's
// core rule being "prefer a gorefactor command over Write/Edit on .go
// files." `history` records what went through the guide; nothing else
// senses when an operator reached past it for a raw edit.
//
// Token-honesty (see the adherence-token-truth finding): `create` saves ~0
// tokens — creating a file emits every line either way — so file CREATION
// is reported SEPARATELY and excluded from the adherence ratio. The ratio
// covers only MODIFICATIONS to existing files, which is where gorefactor's
// token+correctness value actually lives. Crediting create volume would
// inflate adherence with churn that saved nothing.
//
// Attribution is heuristic and honest about it: file-level (not hunk-level)
// and time-bounded (only journal entries newer than the baseline ref
// count). A file gorefactor-touched in range counts even if also
// hand-edited, so this is a ranking signal like blast-radius — never a gate.

func init() {
	registerCommand(Command{
		Name:        "adherence",
		Description: "Audit how much of the current .go diff went through gorefactor vs raw edits (advisory; modifications only, creates reported separately)",
		Usage:       "adherence [--since <ref>] [--json]",
		MinArgs:     0,
		MaxArgs:     0,
		Flags:       map[string]bool{"--since": true, "--json": false},
		Run:         adherenceCommand,
	})
}

// adherenceReport is the token-relevant self-audit. The ratio covers only
// modified existing files; created files are tracked but deliberately kept
// out of it.
type adherenceReport struct {
	Ref                string   `json:"ref"`
	ModifiedTotal      int      `json:"modifiedTotal"`
	ModifiedAttributed int      `json:"modifiedAttributed"`
	ModifiedRaw        []string `json:"modifiedRaw"`
	CreatedTotal       int      `json:"createdTotal"`
	CreatedAttributed  int      `json:"createdAttributed"`
	CreatedRaw         []string `json:"createdRaw"`
}

// ratio is the token-relevant adherence: modified files attributed to a
// gorefactor op ÷ all modified files. ok=false when there were no
// modifications (ratio undefined — don't report 0% on an all-create diff).
func (r adherenceReport) ratio() (float64, bool) {
	if r.ModifiedTotal == 0 {
		return 0, false
	}
	return float64(r.ModifiedAttributed) / float64(r.ModifiedTotal), true
}

// computeAdherence diffs the working tree against `since` (default HEAD),
// classifies each changed .go file as modified vs created, and attributes
// it to the gorefactor journal (time-bounded to entries after the ref).
func computeAdherence(since string) (adherenceReport, error) {
	if since == "" {
		since = "HEAD"
	}
	rep := adherenceReport{Ref: since}

	refTime := gitCommitTime(since) // zero value ⇒ no lower bound
	grfModified, grfCreated := journalFileSets(refTime)

	modified, created, err := changedGoFiles(since)
	if err != nil {
		return rep, err
	}

	for _, f := range modified {
		rep.ModifiedTotal++
		if grfModified[f] {
			rep.ModifiedAttributed++
		} else {
			rep.ModifiedRaw = append(rep.ModifiedRaw, f)
		}
	}
	for _, f := range created {
		rep.CreatedTotal++
		if grfCreated[f] {
			rep.CreatedAttributed++
		} else {
			rep.CreatedRaw = append(rep.CreatedRaw, f)
		}
	}
	sort.Strings(rep.ModifiedRaw)
	sort.Strings(rep.CreatedRaw)
	return rep, nil
}

// journalFileSets returns the sets of paths gorefactor modified vs created,
// counting only journal entries at or after refTime (a zero refTime
// includes everything). Modified = a snapshotted (non-Created) file;
// Created = a JournalFile flagged Created.
func journalFileSets(refTime time.Time) (modified, created map[string]bool) {
	modified, created = map[string]bool{}, map[string]bool{}
	entries, err := orchestrator.LoadJournal()
	if err != nil {
		return modified, created
	}
	for _, e := range entries {
		if !refTime.IsZero() && e.Timestamp.Before(refTime) {
			continue
		}
		for _, f := range e.Files {
			if f.Created {
				created[f.Path] = true
			} else {
				modified[f.Path] = true
			}
		}
	}
	return modified, created
}

// changedGoFiles returns the modified and created .go files in the working
// tree relative to ref (untracked files count as created). Vendored,
// generated, and .gorefactor bookkeeping paths are excluded.
func changedGoFiles(ref string) (modified, created []string, err error) {
	out, err := exec.Command("git", "diff", "--name-status", ref).Output()
	if err != nil {
		return nil, nil, fmt.Errorf("git diff --name-status %s: %w", ref, err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		status := fields[0]
		path := fields[len(fields)-1] // rename → new path is last
		if !adherenceRelevant(path) {
			continue
		}
		switch status[0] {
		case 'A':
			created = append(created, path)
		case 'M', 'R', 'C':
			modified = append(modified, path)
		}
	}
	// Untracked files are brand-new source → created bucket.
	if un, err := exec.Command("git", "ls-files", "--others", "--exclude-standard").Output(); err == nil {
		for _, path := range strings.Split(strings.TrimSpace(string(un)), "\n") {
			if adherenceRelevant(path) {
				created = append(created, path)
			}
		}
	}
	return modified, created, nil
}

// adherenceRelevant reports whether a path is a first-party .go source file
// worth auditing (excludes tests? no — tests are still code we prefer to
// edit via gorefactor; only vendored/generated/bookkeeping paths are cut).
func adherenceRelevant(path string) bool {
	if !strings.HasSuffix(path, ".go") {
		return false
	}
	for _, skip := range []string{"vendor/", ".gorefactor/", "testdata/"} {
		if strings.Contains(path, skip) {
			return false
		}
	}
	return true
}

// gitCommitTime returns the committer time of ref, or the zero time if it
// cannot be resolved (in which case attribution is not time-bounded).
func gitCommitTime(ref string) time.Time {
	out, err := exec.Command("git", "show", "-s", "--format=%cI", ref).Output()
	if err != nil {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(string(out)))
	if err != nil {
		return time.Time{}
	}
	return t
}

func adherenceCommand(args []string) error {
	positional, flags := parseFlags(args, map[string]bool{"--since": true, "--json": false})
	_ = positional
	rep, err := computeAdherence(flags["--since"])
	if err != nil {
		return err
	}
	if flags["--json"] != "" {
		b, _ := json.MarshalIndent(rep, "", "  ")
		fmt.Println(string(b))
		return nil
	}
	printAdherence(rep)
	return nil
}

// printAdherence renders the human report: the headline ratio over
// modifications, the raw-edited files to route through gorefactor next
// time, and creates shown separately as token-neutral.
func printAdherence(rep adherenceReport) {
	fmt.Printf("gorefactor adherence (vs %s)\n", rep.Ref)
	if ratio, ok := rep.ratio(); ok {
		fmt.Printf("  modifications: %d/%d through gorefactor (%.0f%%)\n",
			rep.ModifiedAttributed, rep.ModifiedTotal, ratio*100)
	} else {
		fmt.Printf("  modifications: none (ratio n/a)\n")
	}
	for _, f := range rep.ModifiedRaw {
		fmt.Printf("    ✗ raw edit: %s\n", f)
	}
	fmt.Printf("  created: %d file(s) — token-neutral, excluded from the ratio", rep.CreatedTotal)
	if rep.CreatedTotal > 0 {
		fmt.Printf(" (%d via gorefactor)", rep.CreatedAttributed)
	}
	fmt.Println()
	if len(rep.ModifiedRaw) > 0 {
		fmt.Println("  → route existing-file edits through gorefactor (see the command table in CLAUDE.md)")
	}
	fmt.Println("  note: file-level, time-bounded attribution — a ranking signal, not a proof")
}
