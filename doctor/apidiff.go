package doctor

import (
	"fmt"
	"strings"

	"github.com/chrisophus/gorefactor/analyzer"
)

// APIDiff is the semantic-preservation substrate: it diffs the exported API
// surface of the working tree against BaseRef (reusing analyzer.ComputeAPIDiff,
// the existing api-diff engine) and gates undeclared deltas. Deltas covered by
// a declared api-change intent are demoted to info with the reason cited.
type APIDiff struct{}

// Info implements Substrate. DiffBased: findings are new by construction, so
// the substrate is exempt from baseline builds and baseline marking.
func (APIDiff) Info() SubstrateInfo {
	return SubstrateInfo{Name: "apidiff", Gating: true, DiffBased: true}
}

// Run implements Substrate.
func (APIDiff) Run(ctx RunContext) ([]Finding, error) {
	if ctx.BaseRef == "" {
		return nil, unavailablef("apidiff requires a base ref")
	}
	res, err := analyzer.ComputeAPIDiff(ctx.Root, ctx.BaseRef)
	if err != nil {
		if strings.Contains(err.Error(), "git repository") {
			return nil, unavailablef("%v", err)
		}
		return nil, fmt.Errorf("compute apidiff: %w", err)
	}
	intents, err := LoadIntents(ctx.Root)
	if err != nil {
		return nil, fmt.Errorf("load intents: %w", err)
	}

	var findings []Finding
	for _, entry := range res.Removed {
		sym := firstField(entry)
		findings = append(findings, apiFinding(intents, sym, "api-removed",
			fmt.Sprintf("exported symbol removed: %s", entry), SeverityError))
	}
	for _, ch := range res.Changed {
		findings = append(findings, apiFinding(intents, ch.Symbol, "api-changed",
			fmt.Sprintf("exported signature changed: %s: %s -> %s", ch.Symbol, ch.Old, ch.New), SeverityError))
	}
	// Additions are compatible but still API surface drift; they report as
	// warnings and never gate undeclared.
	for _, entry := range res.Added {
		sym := firstField(entry)
		findings = append(findings, apiFinding(intents, sym, "api-added",
			fmt.Sprintf("exported symbol added: %s", entry), SeverityWarning))
	}
	return findings, nil
}

// apiFinding builds one api-category finding, demoting it to info when a
// declared intent covers the symbol. New is set by construction: every apidiff
// delta is relative to the base ref.
func apiFinding(intents []Intent, symbol, rule, message string, severity Severity) Finding {
	f := Finding{
		File:     symbolDir(symbol),
		Rule:     rule,
		Category: CategoryAPI,
		Severity: severity,
		Message:  message,
		New:      true,
	}
	if in, ok := matchIntent(intents, symbol); ok {
		f.Severity = SeverityInfo
		f.Message += fmt.Sprintf(" (declared: %s)", in.Reason)
	}
	return f
}

// symbolDir extracts the package-dir qualifier from an API symbol
// ("cmd/gorefactor.Foo" -> "cmd/gorefactor") for the Finding.File slot.
func symbolDir(symbol string) string {
	if i := strings.Index(symbol, "."); i > 0 {
		return symbol[:i]
	}
	return ""
}

func firstField(entry string) string {
	if fields := strings.Fields(entry); len(fields) > 0 {
		return fields[0]
	}
	return entry
}
