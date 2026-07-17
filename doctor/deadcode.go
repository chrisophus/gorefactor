package doctor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// Deadcode is the whole-program unused-code substrate (design plan step 4).
// It wraps golang.org/x/tools/cmd/deadcode, whose RTA-based reachability
// analysis sees what the structural linter's name-based dead-code rule cannot:
// exported functions no main package or test ever reaches. Full-run-only —
// whole-program analysis has no meaningful package scope.
type Deadcode struct{}

// Info implements Substrate. Not gating: dead code is warning severity by
// design (plan decision 3b — blocking on dead code trains gate bypass).
func (Deadcode) Info() SubstrateInfo {
	return SubstrateInfo{Name: "deadcode"}
}

// Probe implements prober.
func (Deadcode) Probe(root string) error {
	if _, err := exec.LookPath("deadcode"); err != nil {
		return unavailablef("deadcode not on PATH (go install golang.org/x/tools/cmd/deadcode@latest)")
	}
	return nil
}

// Run implements Substrate. -test keeps test-reachable code live, the
// conservative choice for library modules whose exported API is exercised
// only by tests.
func (d Deadcode) Run(ctx RunContext) ([]Finding, error) {
	if err := d.Probe(ctx.Root); err != nil {
		return nil, fmt.Errorf("probe: %w", err)
	}
	cmd := exec.Command("deadcode", "-json", "-test", "./...")
	cmd.Dir = ctx.Root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, runErr := cmd.Output()
	if runErr != nil && len(bytes.TrimSpace(out)) == 0 {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = runErr.Error()
		}
		return nil, unavailablef("deadcode failed to run: %s", msg)
	}
	return parseDeadcodeJSON(out)
}

// parseDeadcodeJSON maps deadcode's -json output ([]package with dead funcs)
// to findings. Generated functions are dropped here as a courtesy; the merge
// layer's generated-file filter is the real contract.
func parseDeadcodeJSON(out []byte) ([]Finding, error) {
	var packages []struct {
		Name  string `json:"name"`
		Path  string `json:"path"`
		Funcs []struct {
			Name      string `json:"name"`
			Generated bool   `json:"generated"`
			Position  struct {
				File string `json:"file"`
				Line int    `json:"line"`
				Col  int    `json:"col"`
			} `json:"position"`
		} `json:"funcs"`
	}
	if err := json.Unmarshal(out, &packages); err != nil {
		return nil, fmt.Errorf("parse deadcode JSON: %w", err)
	}
	var findings []Finding
	for _, pkg := range packages {
		for _, fn := range pkg.Funcs {
			if fn.Generated {
				continue
			}
			findings = append(findings, Finding{
				File:     fn.Position.File,
				Line:     fn.Position.Line,
				Rule:     "deadcode/unreachable-func",
				Category: CategoryDead,
				Message:  fmt.Sprintf("%s.%s is unreachable from any main package or test", pkg.Path, fn.Name),
			})
		}
	}
	return findings, nil
}
