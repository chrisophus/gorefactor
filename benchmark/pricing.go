package main

import (
	"sort"
	"strings"
)

// pricing.go: per-model token pricing so the corpus can rank
// correctness-per-dollar, not just raw pass rate. The table is a small,
// hand-editable set of USD-per-1M-token rates; lookup is longest-prefix so
// versioned model ids (claude-sonnet-4-6, gpt-4o-mini-2024-07-18) resolve
// to their family without an entry per revision.
//
// Prices are list rates as of early 2026 and WILL drift — they are a
// ranking input, not an invoice. Edit freely; the cost-of-pass ordering is
// what matters, and it is robust to modest absolute error.

// modelPricing is USD per 1,000,000 tokens, split by direction.
type modelPricing struct {
	InPer1M  float64
	OutPer1M float64
}

// priceTable maps a model-id prefix to its rate. Keys are lowercased; the
// longest matching prefix wins so "claude-opus" beats "claude" for
// "claude-opus-4-8".
var priceTable = map[string]modelPricing{
	"claude-opus":   {InPer1M: 15, OutPer1M: 75},
	"claude-sonnet": {InPer1M: 3, OutPer1M: 15},
	"claude-haiku":  {InPer1M: 1, OutPer1M: 5},
	"gpt-4o-mini":   {InPer1M: 0.15, OutPer1M: 0.60},
	"gpt-4o":        {InPer1M: 2.50, OutPer1M: 10},
	"gpt-4.1-mini":  {InPer1M: 0.40, OutPer1M: 1.60},
	"gpt-4.1":       {InPer1M: 2, OutPer1M: 8},
}

// lookupPricing returns the rate for a model id via longest-prefix match,
// and ok=false when the model is unknown (so callers can surface "$?"
// rather than silently pricing at zero).
func lookupPricing(model string) (modelPricing, bool) {
	m := strings.ToLower(strings.TrimSpace(model))
	// Deterministic longest-prefix: sort keys by descending length.
	keys := make([]string, 0, len(priceTable))
	for k := range priceTable {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	for _, k := range keys {
		if strings.HasPrefix(m, k) {
			return priceTable[k], true
		}
	}
	return modelPricing{}, false
}

// costUSD returns the dollar cost of a run given its prompt/completion
// token counts. Unknown models cost 0 and ok=false — the caller decides
// whether to show "$?" or fall back to a token-only ranking.
func costUSD(model string, promptTok, completionTok int) (usd float64, ok bool) {
	p, ok := lookupPricing(model)
	if !ok {
		return 0, false
	}
	return float64(promptTok)/1e6*p.InPer1M + float64(completionTok)/1e6*p.OutPer1M, true
}
