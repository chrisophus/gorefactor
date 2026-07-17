package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Baseline shrink enforcement: the mechanical half of the one-way ratchet.
// `lint --baseline` stops new findings from entering the tree; this check
// stops them from entering the *baseline* — without it, a change could
// re-run --write-baseline to absorb its own regressions and the count-based
// gate would never notice. The comparison is set-based per fingerprint: an
// entry may leave the committed snapshot or shrink its count, never appear
// or grow. Deliberate growth (e.g. a new lint rule baselining its backlog)
// is a visible, auditable act: the caller skips the check for that one
// change (see Makefile/CI wiring) instead of the check quietly tolerating it.

// baselineRatchetCommand compares the working-tree baseline file against the
// same file at ref and fails on any growth.
func baselineRatchetCommand(path, ref string) error {
	current, err := readBaselineFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			if _, oldErr := baselineAtRef(path, ref); oldErr == nil {
				return fmt.Errorf("baseline ratchet: %s exists at %s but was deleted in the working tree — deleting the baseline disables the ratchet", path, ref)
			}
			fmt.Printf("baseline ratchet: no baseline file — nothing to check\n")
			return nil
		}
		return err
	}
	old, err := baselineAtRef(path, ref)
	if err != nil {
		// The file not existing at the base ref means this change introduces
		// the baseline: any content is a strict improvement over none.
		fmt.Printf("baseline ratchet: %s not present at %s (introduced here) — ok\n", path, ref)
		return nil
	}
	growth := baselineGrowth(old, current)
	if len(growth) == 0 {
		fmt.Printf("baseline ratchet: ok — baseline vs %s only shrank or held (%d -> %d entries)\n",
			ref, len(old.Issues), len(current.Issues))
		return nil
	}
	fmt.Printf("baseline ratchet: the committed baseline grew vs %s:\n", ref)
	for _, g := range growth {
		fmt.Printf("  %s\n", g)
	}
	fmt.Println("fix the findings instead of baselining them; if this growth is deliberate (e.g. a new lint rule baselining its pre-existing backlog), skip this check for this one change — see the ratchet step in Makefile/CI")
	return fmt.Errorf("baseline ratchet: %d grown entr%s", len(growth), pluralY(len(growth)))

}

func readBaselineFile(path string) (*baselineFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var bf baselineFile
	if err := json.Unmarshal(data, &bf); err != nil {
		return nil, fmt.Errorf("parse baseline %s: %w", path, err)
	}
	return &bf, nil
}

// baselineAtRef reads the baseline file as committed at a git ref.
func baselineAtRef(path, ref string) (*baselineFile, error) {
	cmd := exec.Command("git", "show", ref+":"+path)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git show %s:%s: %s", ref, path, strings.TrimSpace(stderr.String()))
	}
	var bf baselineFile
	if err := json.Unmarshal(out, &bf); err != nil {
		return nil, fmt.Errorf("parse baseline at %s: %w", ref, err)
	}
	return &bf, nil
}

// baselineGrowth lists every way current grew relative to old: fingerprints
// that are new to the snapshot, and fingerprints whose count increased.
func baselineGrowth(old, current *baselineFile) []string {
	oldCount := map[string]int{}
	for _, e := range old.Issues {
		oldCount[e.Fingerprint] = e.Count
	}
	var growth []string
	for _, e := range current.Issues {
		before, existed := oldCount[e.Fingerprint]
		switch {
		case !existed:
			growth = append(growth, fmt.Sprintf("new entry: %s [%s] (count %d)", e.File, e.Rule, e.Count))
		case e.Count > before:
			growth = append(growth, fmt.Sprintf("count %d -> %d: %s [%s]", before, e.Count, e.File, e.Rule))
		}
	}
	return growth
}

func pluralY(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}
