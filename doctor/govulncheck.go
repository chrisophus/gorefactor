package doctor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// Govulncheck is the call-graph-aware vulnerability substrate (design plan
// step 4). Only reachable vulns report: govulncheck emits findings at module,
// package, and symbol level, and only symbol-level findings — an actual call
// path from this module into the vulnerable function — become doctor findings.
// Full-run-only (call-graph analysis), and it degrades explicitly when the
// vuln DB is unreachable: recorded unavailable, never silently passed.
type Govulncheck struct{}

// Info implements Substrate. Gating: new reachable vulns are sec-category,
// error severity.
func (Govulncheck) Info() SubstrateInfo {
	return SubstrateInfo{Name: "govulncheck", Gating: true}
}

// Probe implements prober. Binary presence only — DB reachability can't be
// checked cheaply and surfaces as ErrUnavailable at run time.
func (Govulncheck) Probe(root string) error {
	if _, err := exec.LookPath("govulncheck"); err != nil {
		return unavailablef("govulncheck not on PATH (go install golang.org/x/vuln/cmd/govulncheck@latest)")
	}
	return nil
}

// Run implements Substrate.
func (g Govulncheck) Run(ctx RunContext) ([]Finding, error) {
	if err := g.Probe(ctx.Root); err != nil {
		return nil, err
	}
	cmd := exec.Command("govulncheck", "-json", "./...")
	cmd.Dir = ctx.Root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, runErr := cmd.Output()
	if runErr != nil && len(bytes.TrimSpace(out)) == 0 {
		// No JSON stream at all: the tool could not run (offline vuln DB,
		// broken toolchain) — a dark sensor, not a clean pass.
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = runErr.Error()
		}
		return nil, unavailablef("govulncheck failed to run: %s", msg)
	}
	return parseGovulncheckJSON(out)
}

// govulnFrame is one frame of a govulncheck finding trace. Frame 0 is the
// vulnerable symbol; the last frame is this module's call site.
type govulnFrame struct {
	Module   string `json:"module"`
	Package  string `json:"package"`
	Function string `json:"function"`
	Receiver string `json:"receiver"`
	Position *struct {
		Filename string `json:"filename"`
		Line     int    `json:"line"`
	} `json:"position"`
}

// parseGovulncheckJSON reads govulncheck's streaming JSON: a sequence of
// {"osv": ...} entries describing vulns and {"finding": ...} entries at
// increasing precision. Symbol-level findings only (trace[0].function set).
func parseGovulncheckJSON(out []byte) ([]Finding, error) {
	dec := json.NewDecoder(bytes.NewReader(out))
	summaries := map[string]string{}
	var findings []Finding
	seen := map[string]bool{}
	for dec.More() {
		var msg struct {
			OSV *struct {
				ID      string `json:"id"`
				Summary string `json:"summary"`
			} `json:"osv"`
			Finding *struct {
				OSV          string        `json:"osv"`
				FixedVersion string        `json:"fixed_version"`
				Trace        []govulnFrame `json:"trace"`
			} `json:"finding"`
		}
		if err := dec.Decode(&msg); err != nil {
			return nil, fmt.Errorf("parse govulncheck JSON: %w", err)
		}
		if msg.OSV != nil {
			summaries[msg.OSV.ID] = msg.OSV.Summary
		}
		f := msg.Finding
		if f == nil || len(f.Trace) == 0 || f.Trace[0].Function == "" {
			continue // module- or package-level: not proven reachable
		}
		vuln := f.Trace[0]
		sym := vuln.Function
		if vuln.Receiver != "" {
			sym = vuln.Receiver + "." + sym
		}
		key := f.OSV + "|" + vuln.Package + "." + sym
		if seen[key] {
			continue
		}
		seen[key] = true
		finding := Finding{
			Rule:     "govulncheck/" + f.OSV,
			Category: CategorySec,
			Message:  reachableVulnMessage(f.OSV, vuln.Package, sym, f.FixedVersion, summaries[f.OSV]),
		}
		if site := f.Trace[len(f.Trace)-1]; site.Position != nil {
			finding.File = site.Position.Filename
			finding.Line = site.Position.Line
		}
		findings = append(findings, finding)
	}
	return findings, nil
}

func reachableVulnMessage(osv, pkg, sym, fixed, summary string) string {
	msg := fmt.Sprintf("%s: %s.%s is reachable", osv, pkg, sym)
	if summary != "" {
		msg += " — " + summary
	}
	if fixed != "" {
		msg += fmt.Sprintf(" (fixed in %s)", fixed)
	}
	return msg
}
