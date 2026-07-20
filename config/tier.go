package config

import "fmt"

// Tier is the lint severity / enablement for a rule in config YAML.
//
// Contract:
//   - error: deterministic CI gate (--fail-on error, default)
//   - warning: actionable debt; normally baselined (--baseline, --fail-on warning)
//   - info: opt-in exploration (--info; hidden by default)
//   - off: rule disabled
//
// With --quiet --fail-only, lint exits silently when nothing reaches --fail-on.
type Tier string

const (
	TierError   Tier = "error"
	TierWarning Tier = "warning"
	TierInfo    Tier = "info"
	TierOff     Tier = "off"
)

// ParseTier validates a YAML tier string.
func ParseTier(s string) (Tier, error) {
	switch Tier(s) {
	case TierError, TierWarning, TierInfo, TierOff:
		return Tier(s), nil
	default:
		return "", fmt.Errorf("invalid rule tier %q (want error, warning, info, or off)", s)
	}
}
