package doctor

import (
	"bytes"
	"os/exec"
	"strings"
)

// prober is implemented by substrates that can cheaply check availability
// without a full run.
type prober interface {
	Probe(root string) error
}

// Preflight probes each substrate's availability without running it, so gate
// consumers can fail fast at campaign start instead of discovering a dark
// sensor at finish (plan decision 10). Substrates without a prober are assumed
// available. The result maps substrate name to its unavailability reason;
// empty means all probed substrates can run.
func Preflight(root string, subs []Substrate) map[string]error {
	out := map[string]error{}
	for _, s := range subs {
		p, ok := s.(prober)
		if !ok {
			continue
		}
		if err := p.Probe(root); err != nil {
			out[s.Info().Name] = err
		}
	}
	return out
}

// Probe implements prober. Beyond binary and config presence it loads the
// config (`golangci-lint config path`), which is what catches a
// version-skewed binary that only fails at run time.
func (Golangci) Probe(root string) error {
	if _, err := exec.LookPath("golangci-lint"); err != nil {
		return unavailablef("golangci-lint not on PATH")
	}
	if !hasGolangciConfig(root) {
		return unavailablef("no .golangci config in %s", root)
	}
	cmd := exec.Command("golangci-lint", "config", "path")
	cmd.Dir = root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return unavailablef("golangci-lint cannot load config: %s", msg)
	}
	return nil
}

// Probe implements prober: apidiff needs a resolvable git HEAD.
func (APIDiff) Probe(root string) error {
	if _, err := resolveSHA(root, "HEAD"); err != nil {
		return unavailablef("%v", err)
	}
	return nil
}
