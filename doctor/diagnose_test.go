package doctor

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDiagnoseMarksNewVsBaseline(t *testing.T) {
	root := gitRepo(t, map[string]string{"a.go": "package a\n"})
	old := Finding{File: "a.go", Rule: "dup", Category: CategoryStruct, Message: "old smell at line 4"}
	fresh := Finding{File: "a.go", Rule: "dup", Category: CategoryStruct, Message: "fresh smell at line 9"}
	sub := &fakeSubstrate{
		info:         SubstrateInfo{Name: "fake", ScopeCapable: true},
		root:         root,
		findings:     []Finding{old, fresh},
		baseFindings: []Finding{old},
	}
	rep, err := Diagnose(Options{Root: root, Substrates: []Substrate{sub}, NoJournal: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Findings) != 2 {
		t.Fatalf("want 2 findings: %+v", rep.Findings)
	}
	var newCount int
	for _, f := range rep.Findings {
		if f.New {
			newCount++
			if f.Message != "fresh smell at line 9" {
				t.Fatalf("wrong finding marked new: %+v", f)
			}
			if f.Severity != SeverityWarning || f.Substrate != "fake" {
				t.Fatalf("defaults not filled: %+v", f)
			}
		}
	}
	if newCount != 1 || rep.NewCount[SeverityWarning] != 1 {
		t.Fatalf("want exactly 1 new warning: %+v", rep.NewCount)
	}
}

func TestDiagnoseRecordsUnavailableSubstrate(t *testing.T) {
	root := gitRepo(t, map[string]string{"a.go": "package a\n"})
	sub := &fakeSubstrate{
		info: SubstrateInfo{Name: "dark", Gating: true},
		err:  errors.New("binary exploded"),
	}
	skip := &fakeSubstrate{
		info: SubstrateInfo{Name: "offline", Gating: true},
		err:  unavailablef("no network"),
	}
	rep, err := Diagnose(Options{Root: root, Substrates: []Substrate{sub, skip}, NoJournal: true})
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]SubstrateState{}
	for _, s := range rep.Substrates {
		states[s.Name] = s.State
	}
	if states["dark"] != SubstrateFailed || states["offline"] != SubstrateSkipped {
		t.Fatalf("availability states wrong: %+v", rep.Substrates)
	}
	if rep.GateOK(true) {
		t.Fatal("gate mode must not pass with dark gating substrates")
	}
	if !rep.GateOK(false) {
		t.Fatal("advisory mode soft-skips dark substrates")
	}
}

func TestDiagnoseGateFailsOnNewError(t *testing.T) {
	root := gitRepo(t, map[string]string{"a.go": "package a\n"})
	sub := &fakeSubstrate{
		info:         SubstrateInfo{Name: "sec", Gating: true},
		root:         root,
		findings:     []Finding{{File: "a.go", Rule: "gosec", Category: CategorySec, Message: "boom"}},
		baseFindings: []Finding{},
	}

	rep, err := Diagnose(Options{Root: root, Substrates: []Substrate{sub}, NoJournal: true})
	if err != nil {
		t.Fatal(err)
	}
	if rep.GateOK(false) || len(rep.NewErrors()) != 1 {
		t.Fatalf("new sec finding must gate: %+v", rep.Findings)
	}
}

func TestDiagnoseWritesJournal(t *testing.T) {
	root := gitRepo(t, map[string]string{"a.go": "package a\n"})
	sub := &fakeSubstrate{info: SubstrateInfo{Name: "fake"}}
	if _, err := Diagnose(Options{Root: root, Substrates: []Substrate{sub}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".gorefactor", journalFileName)); err != nil {
		t.Fatalf("journal not written: %v", err)
	}
}

func TestDiagnoseScopedSkipsFullRunOnlySubstrates(t *testing.T) {
	root := gitRepo(t, map[string]string{"a.go": "package a\n"})
	// Uncommitted change so the scope is non-empty.
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\n\nfunc A() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	whole := &fakeSubstrate{info: SubstrateInfo{Name: "wholeprogram"}}
	rep, err := Diagnose(Options{Root: root, Substrates: []Substrate{whole}, Scoped: true, NoJournal: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Scope) == 0 {
		t.Fatalf("scope should include the changed package: %+v", rep)
	}
	if rep.Substrates[0].State != SubstrateSkipped {
		t.Fatalf("full-run-only substrate must be skipped in scoped runs: %+v", rep.Substrates)
	}
	if rep.FixedCount != nil {
		t.Fatal("FixedCount is meaningless on scoped runs")
	}
}
