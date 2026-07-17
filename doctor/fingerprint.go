package doctor

import "strings"

// fingerprint identifies a finding independently of the exact line it sits on,
// so a finding that merely shifts when unrelated code is added above it is
// still recognised as pre-existing. Same scheme as the lint ratchet's
// issueFingerprint (cmd/gorefactor/cmd_lint_baseline.go, package main, hence
// not importable): file + rule + message with all digit runs collapsed.
func fingerprint(f Finding) string {
	return f.File + "\x00" + f.Rule + "\x00" + normalizeMessage(f.Message)
}

// normalizeMessage replaces every maximal run of ASCII digits with a single
// '#', turning "is 80 lines (threshold 75, line 98)" into
// "is # lines (threshold #, line #)" — stable parts (symbols, paths, phrasing)
// survive, volatile numbers do not.
func normalizeMessage(msg string) string {
	var b strings.Builder
	b.Grow(len(msg))
	inDigits := false
	for i := 0; i < len(msg); i++ {
		c := msg[i]
		if c >= '0' && c <= '9' {
			if !inDigits {
				b.WriteByte('#')
				inDigits = true
			}
			continue
		}
		inDigits = false
		b.WriteByte(c)
	}
	return b.String()
}
