package config

import "fmt"

// Tier is the lint severity / enablement for a rule in config YAML.
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
