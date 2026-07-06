package main

import (
	"fmt"
	"strings"
)

// sweep.go: the model×harness-mode sweep and its cost-of-pass aggregation
// (Slice 3c). The headline metric is correctness-per-dollar, not raw pass
// rate: a cheaper model that passes as often ranks strictly better. Pure
// aggregation lives here (unit-tested without the LLM); the runner in
// agent_corpus.go feeds it observed task results.

// sweepCell is one (provider, model, mode) configuration the corpus runs
// the whole task set under.
type sweepCell struct {
	provider string
	model    string
	mode     string // agentic | single-shot
}

// splitList parses a comma-separated flag value, falling back to a single
// default element when empty.
func splitList(csv, fallback string) []string {
	csv = strings.TrimSpace(csv)
	if csv == "" {
		return []string{fallback}
	}
	var out []string
	for _, p := range strings.Split(csv, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return []string{fallback}
	}
	return out
}

// providerForModel infers the provider from a model id, so a mixed model
// sweep routes each id to the right API without a per-model provider flag.
// Falls back to the caller's default for unrecognized ids.
func providerForModel(model, fallback string) string {
	m := strings.ToLower(model)
	switch {
	case strings.HasPrefix(m, "claude"):
		return "anthropic"
	case strings.HasPrefix(m, "gpt"), strings.HasPrefix(m, "o1"), strings.HasPrefix(m, "o3"), strings.HasPrefix(m, "o4"):
		return "openai"
	default:
		return fallback
	}
}

// buildCells expands the opts' model and mode sweeps into the cartesian
// set of cells to run. A bare single-model/single-mode run yields one cell
// (preserving the pre-sweep behavior).
func buildCells(o corpusOpts) []sweepCell {
	models := splitList(o.models, o.model)
	modes := splitList(o.modes, "agentic")
	var cells []sweepCell
	for _, m := range models {
		for _, mode := range modes {
			cells = append(cells, sweepCell{provider: providerForModel(m, o.provider), model: m, mode: mode})
		}
	}
	return cells
}

// cellSummary is the aggregate cost/correctness of one cell over the task
// set. It is accumulated task-by-task via observe and then queried for the
// derived cost-of-pass metrics.
type cellSummary struct {
	provider, model, mode string
	n, passed             int
	promptTok, complTok   int
	wallMs                int64
	usd                   float64
	costKnown             bool
}

// observe folds one task result into the summary: pass/total counts, token
// totals, wall-clock, and (when the model is priced) dollar cost.
func (s *cellSummary) observe(model string, pass bool, res taskResult) {
	s.n++
	if pass {
		s.passed++
	}
	s.promptTok += res.metrics.PromptTokens
	s.complTok += res.metrics.CompletionTokens
	s.wallMs += res.wallMs
	if usd, ok := costUSD(model, res.metrics.PromptTokens, res.metrics.CompletionTokens); ok {
		s.usd += usd
		s.costKnown = true
	}
}

func (s cellSummary) passRate() float64 {
	if s.n == 0 {
		return 0
	}
	return float64(s.passed) / float64(s.n)
}

func (s cellSummary) totalTokens() int { return s.promptTok + s.complTok }

// costOfPassUSD is total dollar cost divided by pass rate — the expected
// spend to buy one passing solution under naive retry. ok is false when
// the model is unpriced or nothing passed (division by zero).
func (s cellSummary) costOfPassUSD() (float64, bool) {
	pr := s.passRate()
	if !s.costKnown || pr == 0 {
		return 0, false
	}
	return s.usd / pr, true
}

// costOfPassTokens is the token analogue of costOfPassUSD, always available
// when at least one task passed.
func (s cellSummary) costOfPassTokens() (float64, bool) {
	pr := s.passRate()
	if pr == 0 {
		return 0, false
	}
	return float64(s.totalTokens()) / pr, true
}

// dollars renders a USD amount, or "$?" when the model is unpriced.
func dollars(v float64, known bool) string {
	if !known {
		return "$?"
	}
	return fmt.Sprintf("$%.4f", v)
}

// printCellSummary prints the one-line roll-up under a cell's task table.
func printCellSummary(s cellSummary) {
	copUSD, okUSD := s.costOfPassUSD()
	copTok, okTok := s.costOfPassTokens()
	copTokStr := "n/a"
	if okTok {
		copTokStr = fmt.Sprintf("%.0f", copTok)
	}
	fmt.Printf("%d/%d passed (%.0f%%)  in=%d out=%d tok  %s  cost/pass=%s  tok/pass=%s  %dms\n",
		s.passed, s.n, s.passRate()*100, s.promptTok, s.complTok,
		dollars(s.usd, s.costKnown), dollars(copUSD, okUSD), copTokStr, s.wallMs)
}

// printSweepMatrix prints the cross-cell comparison table — the operational
// form of the "cheap-model delegation wins" question.
func printSweepMatrix(summaries []cellSummary) {
	fmt.Printf("\n%-22s  %-12s  %-8s  %-10s  %-11s  %-12s  %s\n",
		"model", "mode", "pass", "cost", "cost/pass", "tok/pass", "avg_ms")
	fmt.Println(strings.Repeat("-", 96))
	for _, s := range summaries {
		copUSD, okUSD := s.costOfPassUSD()
		copTok, okTok := s.costOfPassTokens()
		copTokStr := "n/a"
		if okTok {
			copTokStr = fmt.Sprintf("%.0f", copTok)
		}
		avgMs := int64(0)
		if s.n > 0 {
			avgMs = s.wallMs / int64(s.n)
		}
		fmt.Printf("%-22s  %-12s  %-8s  %-10s  %-11s  %-12s  %d\n",
			trimTo(s.model, 22), s.mode,
			fmt.Sprintf("%d/%d", s.passed, s.n),
			dollars(s.usd, s.costKnown), dollars(copUSD, okUSD), copTokStr, avgMs)
	}
	fmt.Println(strings.Repeat("-", 96))
	fmt.Println("cost/pass = total cost ÷ pass rate (expected spend per passing solution under naive retry)")
}
