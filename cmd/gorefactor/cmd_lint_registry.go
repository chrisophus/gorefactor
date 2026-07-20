package main

// knownLintRuleNames returns registered lint rule names for config validation.
func knownLintRuleNames() map[string]struct{} {
	names := make(map[string]struct{}, len(defaultLintRules()))
	for _, r := range defaultLintRules() {
		names[r.Name()] = struct{}{}
	}
	return names
}
