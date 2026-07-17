package doctor

import "testing"

func TestIntentRoundTrip(t *testing.T) {
	root := t.TempDir()
	if err := AddIntent(root, Intent{Type: IntentAPIChange, Scope: "analyzer", Reason: "test"}); err != nil {
		t.Fatal(err)
	}
	intents, err := LoadIntents(root)
	if err != nil || len(intents) != 1 {
		t.Fatalf("expected 1 intent, got %v (%v)", intents, err)
	}
	if intents[0].Created.IsZero() {
		t.Fatal("Created should be stamped")
	}
	if err := ClearIntents(root); err != nil {
		t.Fatal(err)
	}
	if intents, _ = LoadIntents(root); len(intents) != 0 {
		t.Fatalf("expected no intents after clear, got %v", intents)
	}
}

func TestIntentRequiresScope(t *testing.T) {
	if err := AddIntent(t.TempDir(), Intent{Type: IntentAPIChange}); err == nil {
		t.Fatal("scope-less intent must be rejected: blanket declarations bypass the gate")
	}
}

func TestIntentMatchesBoundaries(t *testing.T) {
	in := Intent{Type: IntentAPIChange, Scope: "analyzer"}
	for sym, want := range map[string]bool{
		"analyzer":                true,
		"analyzer.ComputeAPIDiff": true,
		"analyzer/sub.Thing":      true,
		"analyzer2.Foo":           false, // prefix must respect path/symbol boundaries
		"cmd/analyzer.Foo":        false,
	} {
		if got := in.Matches(sym); got != want {
			t.Errorf("Matches(%q) = %v, want %v", sym, got, want)
		}
	}
}
