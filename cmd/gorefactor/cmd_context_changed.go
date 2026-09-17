package main

import (
	"fmt"
	"strconv"

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
	revisions, err := positiveFlag(flags, "--history-revisions")
	if err != nil {
		return err
	}
	spans, err := positiveFlag(flags, "--history-spans")
	if err != nil {
		return err
	}
	env, err := changectx.Build(changectx.Options{
		Root:                 root,
		BaseRef:              ref,
		Version:              version.Version(),
		HistoryRevisions:     revisions,
		HistoryRangesPerFile: spans,
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

// positiveFlag reads a count flag. Unset is zero, which Build reads as its own
// default; zero or negative is refused rather than silently emptying a role.
func positiveFlag(flags map[string]string, name string) (int, error) {
	raw := flags[name]
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, usageErrorf("%s takes a whole number (got %q)", name, raw)
	}
	if n < 1 {
		return 0, usageErrorf("%s must be at least 1 (got %d); omit it for the default", name, n)
	}
	return n, nil
}
