package main

import "testing"

func TestCalculatePriorityRanksClearInputsOutputs(t *testing.T) {
	rec := ExtractionRecommendation{
		Complexity:     5,
		StatementCount: 10,
		ReadVars:       []string{"x", "y"},
		WriteVars:      []string{"result"},
		Extractable:    true,
	}
	p := calculatePriority(rec)
	if p <= 0 || p > 10 {
		t.Fatalf("priority out of 1..10 bound: %d", p)
	}
}

func TestSuggestMethodNameNonEmpty(t *testing.T) {
	rec := ExtractionRecommendation{
		Complexity:     4,
		StatementCount: 8,
		ReadVars:       []string{"data"},
		WriteVars:      []string{"err"},
	}
	name := suggestMethodName(rec)
	if name == "" {
		t.Fatal("expected non-empty suggested name")
	}
}
