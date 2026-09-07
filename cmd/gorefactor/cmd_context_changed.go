package main

import (
	"fmt"

	"github.com/chrisophus/gorefactor/internal/changectx"
	"github.com/chrisophus/gorefactor/version"
)

// changedContextCommand serves `context --changed <ref>`: the context envelope
// for a whole change, for a reviewer that already has the diff. It is a
// different job from the per-symbol pack above it, and the difference that
// matters is the budget. The pack trims to fit one prompt; the envelope leaves
// ranking and truncation to whatever consumes it, so trimming here would throw
// away context that consumer had room for.
func changedContextCommand(ref, root string, positional []string, flags map[string]string) error {
	if len(positional) > 0 {
		return usageErrorf("context --changed takes no symbol argument (got %q)", positional[0])
	}
	if flags["--budget"] != "" {
		return usageErrorf("--budget does not apply to --changed; expansions are emitted whole for the consumer to rank")
	}
	env, err := changectx.Build(changectx.Options{
		Root:    root,
		BaseRef: ref,
		Version: version.Version(),
	})
	if err != nil {
		return err
	}
	if flags["--json"] != "" {
		emitEnvelope(true, "", env)
		return nil
	}
	fmt.Print(changectx.Summary(env))
	return nil
}
