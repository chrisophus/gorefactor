package main

import (
	"math"
	"testing"
)

func TestLookupPricingLongestPrefix(t *testing.T) {
	// "claude-opus-4-8" must resolve to the opus rate, not a shorter match.
	p, ok := lookupPricing("claude-opus-4-8")
	if !ok || p.InPer1M != 15 || p.OutPer1M != 75 {
		t.Fatalf("opus mismatch: %+v ok=%v", p, ok)
	}
	// gpt-4o-mini must beat gpt-4o.
	p, ok = lookupPricing("gpt-4o-mini-2024-07-18")
	if !ok || p.InPer1M != 0.15 {
		t.Fatalf("gpt-4o-mini mismatch: %+v ok=%v", p, ok)
	}
	if _, ok := lookupPricing("mystery-model"); ok {
		t.Error("unknown model should not resolve")
	}
}

func TestCostUSD(t *testing.T) {
	// 1M prompt + 1M completion on sonnet = $3 + $15 = $18.
	usd, ok := costUSD("claude-sonnet-4-6", 1_000_000, 1_000_000)
	if !ok || !approx(usd, 18) {
		t.Fatalf("got %v ok=%v, want 18", usd, ok)
	}
	if _, ok := costUSD("mystery", 100, 100); ok {
		t.Error("unknown model should report ok=false")
	}
}

func TestProviderForModel(t *testing.T) {
	cases := map[string]string{
		"claude-sonnet-4-6": "anthropic",
		"gpt-4o-mini":       "openai",
		"o3-mini":           "openai",
		"weird-local":       "fallbackprov",
	}
	for model, want := range cases {
		if got := providerForModel(model, "fallbackprov"); got != want {
			t.Errorf("providerForModel(%q) = %q, want %q", model, got, want)
		}
	}
}

func TestBuildCellsCartesian(t *testing.T) {
	cells := buildCells(corpusOpts{
		model: "claude-sonnet-4-6", provider: "anthropic",
		models: "claude-haiku-4-5,gpt-4o-mini", modes: "agentic,single-shot",
	})
	if len(cells) != 4 {
		t.Fatalf("expected 2x2=4 cells, got %d", len(cells))
	}
	// Provider inferred per model.
	for _, c := range cells {
		if c.model == "gpt-4o-mini" && c.provider != "openai" {
			t.Errorf("gpt model routed to %q, want openai", c.provider)
		}
		if c.model == "claude-haiku-4-5" && c.provider != "anthropic" {
			t.Errorf("claude model routed to %q, want anthropic", c.provider)
		}
	}
	// Bare single config → exactly one cell.
	if got := buildCells(corpusOpts{model: "m", provider: "p"}); len(got) != 1 {
		t.Errorf("bare config should yield 1 cell, got %d", len(got))
	}
}

func TestCellSummaryCostOfPass(t *testing.T) {
	var s cellSummary
	mk := func(pt, ct int) taskResult {
		return taskResult{metrics: runMetrics{PromptTokens: pt, CompletionTokens: ct}, wallMs: 100}
	}
	// 2 of 4 pass; each task 1M in + 1M out on sonnet = $18/task, $72 total.
	s.observe("claude-sonnet-4-6", true, mk(1_000_000, 1_000_000))
	s.observe("claude-sonnet-4-6", false, mk(1_000_000, 1_000_000))
	s.observe("claude-sonnet-4-6", true, mk(1_000_000, 1_000_000))
	s.observe("claude-sonnet-4-6", false, mk(1_000_000, 1_000_000))

	if s.passRate() != 0.5 {
		t.Fatalf("pass rate %v, want 0.5", s.passRate())
	}
	// cost/pass = $72 / 0.5 = $144.
	cop, ok := s.costOfPassUSD()
	if !ok || !approx(cop, 144) {
		t.Fatalf("cost/pass %v ok=%v, want 144", cop, ok)
	}
	// tok/pass = 8M / 0.5 = 16M.
	copTok, ok := s.costOfPassTokens()
	if !ok || !approx(copTok, 16_000_000) {
		t.Fatalf("tok/pass %v ok=%v, want 16e6", copTok, ok)
	}
}

func TestCellSummaryZeroPassNoDivZero(t *testing.T) {
	var s cellSummary
	s.observe("claude-sonnet-4-6", false, taskResult{metrics: runMetrics{PromptTokens: 100}})
	if _, ok := s.costOfPassUSD(); ok {
		t.Error("zero passes must report cost/pass unavailable, not divide by zero")
	}
	if _, ok := s.costOfPassTokens(); ok {
		t.Error("zero passes must report tok/pass unavailable")
	}
}

func TestCellSummaryUnpricedModel(t *testing.T) {
	var s cellSummary
	s.observe("mystery-model", true, taskResult{metrics: runMetrics{PromptTokens: 1000, CompletionTokens: 1000}})
	if s.costKnown {
		t.Error("unpriced model should leave costKnown false")
	}
	if _, ok := s.costOfPassUSD(); ok {
		t.Error("unpriced model should report cost/pass unavailable")
	}
	// Token-based cost-of-pass is still available (pricing-independent).
	if _, ok := s.costOfPassTokens(); !ok {
		t.Error("tok/pass should be available regardless of pricing")
	}
}

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }
