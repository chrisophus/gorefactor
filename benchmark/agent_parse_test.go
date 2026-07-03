package main

import (
	goparser "go/parser"
	"go/token"
	"strings"
	"testing"
)

func TestClassifyOutcome(t *testing.T) {
	fixed := `<<<RUN_METRICS {"outcome":"fixed","steps":3,"local_tokens":52664} RUN_METRICS>>>`
	punt := `<<<RUN_METRICS {"outcome":"punt","steps":30,"local_tokens":400000} RUN_METRICS>>>`
	friction := "<<<FRICTION_REPORT\n{\"missing_command\":\"add-field\"}\nFRICTION_REPORT>>>\n" + fixed

	cases := map[string]expectedOutcome{
		fixed:                                    outEfficient,
		friction:                                 outFriction,
		punt:                                     outFail,
		"no blocks at all":                       outError,
		`<<<RUN_METRICS not-json RUN_METRICS>>>`: outError,
	}
	for in, want := range cases {
		if got := classifyOutcome(in); got != want {
			t.Errorf("classifyOutcome(%.40q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseRunMetrics(t *testing.T) {
	rm, ok := parseRunMetrics(`prefix <<<RUN_METRICS {"outcome":"fixed","steps":7,"local_tokens":123} RUN_METRICS>>> suffix`)
	if !ok || rm.Outcome != "fixed" || rm.Steps != 7 || rm.LocalTokens != 123 {
		t.Fatalf("parseRunMetrics wrong: %+v ok=%v", rm, ok)
	}
	if _, ok := parseRunMetrics("nothing here"); ok {
		t.Fatalf("expected ok=false when no block present")
	}
}

func TestPuntCapabilityGap(t *testing.T) {
	withGap := "<<<PUNT_REPORT\n{\"kind\":\"explicit\",\"capability_gap\":{\"missing_command\":\"add-field\"}}\nPUNT_REPORT>>>"
	judgement := "<<<PUNT_REPORT\n{\"kind\":\"autopunt:judgement\"}\nPUNT_REPORT>>>"
	if !puntHasCapabilityGap(withGap) {
		t.Fatal("expected capability gap to be detected")
	}
	if puntHasCapabilityGap(judgement) {
		t.Fatal("judgement punt must not report a capability gap")
	}
}

// TestCorpusFixturesAreValid guards the corpus itself: every fixture must
// carry a go.mod and every .go file must parse, so a broken fixture is caught
// here rather than as a spurious agent failure during a live run.
func TestCorpusFixturesAreValid(t *testing.T) {
	ids := map[string]bool{}
	for _, task := range agentTasks() {
		if ids[task.ID] {
			t.Fatalf("duplicate task id %q", task.ID)
		}
		ids[task.ID] = true
		if strings.TrimSpace(task.Spec) == "" {
			t.Fatalf("%s: empty spec", task.ID)
		}
		if _, ok := task.Fixture["go.mod"]; !ok {
			t.Fatalf("%s: fixture missing go.mod", task.ID)
		}
		switch task.Expected {
		case outEfficient, outFriction, outFail:
		default:
			t.Fatalf("%s: invalid expected outcome %q", task.ID, task.Expected)
		}
		for name, content := range task.Fixture {
			if !strings.HasSuffix(name, ".go") {
				continue
			}
			if _, err := goparser.ParseFile(token.NewFileSet(), name, content, 0); err != nil {
				t.Fatalf("%s: fixture %s does not parse: %v", task.ID, name, err)
			}
		}
	}
}
