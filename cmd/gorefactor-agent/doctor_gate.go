package main

import (
	"flag"
	"fmt"
	"strings"

	"github.com/chrisophus/gorefactor/doctor"
)

// doctorGateMode is the -doctor-gate flag: "advisory" (default; report new
// error-severity findings on gate runs without blocking — the design plan's
// advisory-first rollout), "hard" (finish fails on new error-severity findings
// or dark gating substrates), or "off".
var doctorGateMode = "advisory"

// gateSubstrates composes the agent gate's substrate set: the library substrates that can produce
// error-severity findings, plus the full-run-only tier (deadcode, govulncheck) that scoped runs
// skip automatically and the campaign-completion full pass executes. The structural linter
// (warning-severity by design) stays in gorefactor's own doctor command.
func gateSubstrates() []doctor.Substrate {
	return []doctor.Substrate{
		doctor.Golangci{},
		doctor.APIDiff{},
		doctor.Temporal{},
		doctor.Deadcode{},
		doctor.Govulncheck{},
	}

}

var doctorGateFlag = flag.String("doctor-gate", "advisory", "doctor findings gate on finish: advisory (report new error-severity findings, never block), hard (block on them and on dark gating substrates), off")

func applyDoctorGateFlag() { doctorGateMode = *doctorGateFlag }

// runDoctorGate is the third leg of the finish gate (design plan: finish =
// build && test && no new error-severity findings in a scoped doctor run).
// full=true is the campaign-completion whole-repo pass. Returns (blocking,
// advisory): blocking is non-empty only in hard mode; advisory carries the
// same findings as feedback the model sees without being blocked.
func runDoctorGate(dir string, full bool) (blocking, advisory string) {
	if doctorGateMode == "off" {
		return "", ""
	}
	rep, err := doctor.Diagnose(doctor.Options{
		Root:       dir,
		BaseRef:    "HEAD",
		Substrates: gateSubstrates(),
		Scoped:     !full,
	})
	if err != nil {
		return splitByMode(fmt.Sprintf("doctor gate could not run: %v", err))
	}
	var b strings.Builder
	for _, f := range rep.NewErrors() {
		loc := f.File
		if f.Line > 0 {
			loc = fmt.Sprintf("%s:%d", f.File, f.Line)
		}
		fmt.Fprintf(&b, "[%s/%s] %s: %s\n", f.Rule, f.Category, loc, f.Message)
	}
	if doctorGateMode == "hard" {
		for _, s := range rep.DarkSubstrates() {
			fmt.Fprintf(&b, "gating substrate %s did not run: %s\n", s.Name, s.Detail)
		}
	}
	return splitByMode(b.String())
}

// splitByMode routes gate text to the blocking or advisory channel.
func splitByMode(text string) (blocking, advisory string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", ""
	}
	if doctorGateMode == "hard" {
		return text, ""
	}
	return "", "doctor advisory (new findings that would fail the hard gate):\n" + text
}

// preflightDoctorGate fails fast when a gating substrate cannot run in hard
// mode — discovering a dark sensor at finish, after a full campaign of work,
// is the worst place to learn it.
func preflightDoctorGate(dir string) error {
	if doctorGateMode != "hard" {
		return nil
	}
	for name, err := range doctor.Preflight(dir, gateSubstrates()) {
		return fmt.Errorf("doctor gate preflight: substrate %s unavailable (fix it or run with -doctor-gate advisory): %w", name, err)
	}
	return nil
}
