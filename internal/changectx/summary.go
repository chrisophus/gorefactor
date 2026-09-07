package changectx

import (
	"fmt"
	"sort"
	"strings"
)

// Summary renders an envelope as a few lines for a person at a terminal. The
// JSON is what the consumer reads; this exists so a human can see whether the
// provider found what they expected before wiring it into anything.
func Summary(env *Envelope) string {
	var b strings.Builder
	fmt.Fprintf(&b, "context envelope v%d  base %s  provider %s %s\n",
		env.SchemaVersion, shortSHA(env.BaseSHA), env.Provider.Name, env.Provider.Version)

	classes := map[string]int{}
	symbols := 0
	generated := 0
	for _, f := range env.Files {
		classes[string(f.Class)]++
		symbols += len(f.Symbols)
		if f.Generated {
			generated++
		}
	}
	fmt.Fprintf(&b, "files: %d (%s)\n", len(env.Files), counts(classes))
	if generated > 0 {
		fmt.Fprintf(&b, "generated: %d file(s) flagged for summary rather than reading\n", generated)
	}
	fmt.Fprintf(&b, "symbols: %d\n", symbols)

	roles := map[string]int{}
	for _, e := range env.Expansions {
		roles[string(e.Role)]++
	}
	fmt.Fprintf(&b, "expansions: %d (%s)\n", len(env.Expansions), counts(roles))
	for _, n := range env.Notes {
		fmt.Fprintf(&b, "note: %s\n", n)
	}
	return b.String()
}

// counts renders a count map with its keys sorted, so the same envelope prints
// the same line every time.
func counts(m map[string]int) string {
	if len(m) == 0 {
		return "none"
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s %d", k, m[k]))
	}
	return strings.Join(parts, ", ")
}

func shortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	if sha == "" {
		return "(none)"
	}
	return sha
}
