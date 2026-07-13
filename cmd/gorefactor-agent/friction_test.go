package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFrictionRoundTrip: a friction call on a completed task records a row
// in .gorefactor/friction.jsonl, emits a <<<FRICTION_REPORT>>> block, does
// not terminate the run, and the run still ends green via finish.
func TestFrictionRoundTrip(t *testing.T) {
	dir := newSampleRepo(t)
	mock := &mockToolProvider{script: []chatMessage{
		asstCall("friction", `{"missing_command":"change-signature --add-param",`+
			`"suggested_syntax":"change-signature sample.go Sum --add-param \"ctx context.Context\"",`+
			`"workaround_steps":"replace_code sig\nreplace_code caller 1\nreplace_code caller 2",`+
			`"estimated_steps_saved":3}`),
		asstCall("finish", `{}`),
	}}

	var log bytes.Buffer
	if err := RunAgenticDriver(context.Background(), mock,
		Config{Spec: "add a ctx param to Sum", Dir: dir, MaxIter: 8, Out: &log}); err != nil {
		t.Fatalf("expected clean finish after friction, got %v\nlog:\n%s", err, log.String())
	}

	if !strings.Contains(log.String(), "<<<FRICTION_REPORT") {
		t.Fatalf("no FRICTION_REPORT block in log:\n%s", log.String())
	}
	rows := readFriction(t, dir)
	if len(rows) != 1 {
		t.Fatalf("expected exactly 1 friction row, got %d: %+v", len(rows), rows)
	}
	r := rows[0]
	if r.MissingCommand != "change-signature --add-param" || r.EstimatedStepsSaved != 3 {
		t.Fatalf("friction row not persisted faithfully: %+v", r)
	}
	if len(r.WorkaroundSteps) != 3 {
		t.Fatalf("workaround_steps not split into 3 lines: %+v", r.WorkaroundSteps)
	}
	if r.TS == "" {
		t.Fatalf("friction row missing timestamp: %+v", r)
	}
}

// TestFrictionRequiresMissingCommand: a friction call with no missing_command
// is a no-op error string and records nothing.
func TestFrictionRequiresMissingCommand(t *testing.T) {
	dir := newSampleRepo(t)
	mock := &mockToolProvider{script: []chatMessage{
		asstCall("friction", `{"suggested_syntax":"whatever"}`),
		asstCall("finish", `{}`),
	}}
	var log bytes.Buffer
	if err := RunAgenticDriver(context.Background(), mock,
		Config{Spec: "x", Dir: dir, MaxIter: 8, Out: &log}); err != nil {
		t.Fatalf("run should still finish: %v", err)
	}
	if rows := readFriction(t, dir); len(rows) != 0 {
		t.Fatalf("friction without missing_command should record nothing, got %+v", rows)
	}
}

// TestCapabilityGapPunt: punting WITH missing_command populates the
// capability_gap field, logs a capability_gap corpus row, and files a
// tool_gap note.
func TestCapabilityGapPunt(t *testing.T) {
	dir := newSampleRepo(t)
	mock := &mockToolProvider{script: []chatMessage{
		asstCall("punt", `{"reason":"no command to change return types",`+
			`"missing_command":"change-signature --change-returns",`+
			`"suggested_syntax":"change-signature sample.go Sum --change-returns \"(int, error)\""}`),
	}}
	var log bytes.Buffer
	err := RunAgenticDriver(context.Background(), mock,
		Config{Spec: "make Sum return an error", Dir: dir, MaxIter: 8, Out: &log})
	if err == nil || !strings.Contains(err.Error(), "PUNT") {
		t.Fatalf("expected a punt, got %v\nlog:\n%s", err, log.String())
	}
	rep := extractPuntReport(t, log.String())
	if rep.CapabilityGap == nil {
		t.Fatalf("expected capability_gap on the punt report, got nil: %+v", rep)
	}
	if rep.CapabilityGap.MissingCommand != "change-signature --change-returns" {
		t.Fatalf("wrong missing_command: %+v", rep.CapabilityGap)
	}

	// Corpus carries a capability_gap row.
	gapRow := false
	for _, e := range readCorpus(t, dir) {
		if e.Kind == failCapabilityGap && strings.Contains(e.Op, "change-returns") {
			gapRow = true
		}
	}
	if !gapRow {
		t.Fatalf("capability_gap not recorded in the failure corpus")
	}

	// A tool_gap note is filed.
	if !strings.Contains(loadNotes(dir), "[tool_gap]") {
		t.Fatalf("expected a tool_gap note, notes:\n%s", loadNotes(dir))
	}
}

// TestJudgementPuntHasNoGap: punting WITHOUT missing_command leaves the
// capability_gap field nil, so a judgement punt block is unchanged.
func TestJudgementPuntHasNoGap(t *testing.T) {
	dir := newSampleRepo(t)
	mock := &mockToolProvider{script: []chatMessage{
		asstCall("punt", `{"reason":"this needs human judgement about the algorithm"}`),
	}}
	var log bytes.Buffer
	err := RunAgenticDriver(context.Background(), mock,
		Config{Spec: "rewrite the algorithm to be faster", Dir: dir, MaxIter: 8, Out: &log})
	if err == nil || !strings.Contains(err.Error(), "PUNT") {
		t.Fatalf("expected a punt, got %v", err)
	}
	rep := extractPuntReport(t, log.String())
	if rep.CapabilityGap != nil {
		t.Fatalf("judgement punt must not carry a capability_gap: %+v", rep.CapabilityGap)
	}
	if strings.Contains(log.String(), `"capability_gap"`) {
		t.Fatalf("capability_gap key should be omitted from a judgement punt block")
	}
}

// readFriction parses every line of the friction corpus under dir.
func readFriction(t *testing.T, dir string) []FrictionReport {
	t.Helper()
	f, err := os.Open(filepath.Join(dir, frictionRelPath))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	defer f.Close()
	var out []FrictionReport
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var r FrictionReport
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("friction line not JSON: %v (%q)", err, line)
		}
		out = append(out, r)
	}
	return out
}
