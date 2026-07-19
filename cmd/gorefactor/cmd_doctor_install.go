package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// doctor install: the prevention loop (design plan step 6). Emits doctor's
// rule expectations into agent context files so the generating model sees the
// rules before writing code — findings should trend toward zero at generation
// time, measured from the doctor findings journal. The emitted section is
// generated from the live rule registry, so it cannot drift from what doctor
// actually enforces.

const doctorRulesSentinel = "<!-- gorefactor:doctor-rules -->"

func doctorInstallCommand(args []string) error {
	target := "claude.md"
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--target":
			if i+1 < len(args) {
				target = args[i+1]
				i++
			}
		case strings.HasPrefix(a, "--target="):
			target = strings.TrimPrefix(a, "--target=")
		}
	}
	paths := resolveAgentRuleTargets(target)
	if len(paths) == 0 {
		return fmt.Errorf("unknown --target %q (want: claude.md, cursor, agents.md, all)", target)
	}
	content := renderDoctorRules()
	for _, path := range paths {
		if err := appendMarkedSection(path, doctorRulesSentinel, content); err != nil {
			return err
		}
	}
	return nil
}

// appendMarkedSection appends content to path unless its sentinel is already
// present — same idempotence contract as init-agent-rules.
func appendMarkedSection(path, sentinel, content string) error {
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if strings.Contains(string(existing), sentinel) {
		fmt.Printf("%s: already contains gorefactor doctor rules (skipping)\n", path)
		return nil
	}
	out := content
	if len(existing) > 0 {
		out = strings.TrimRight(string(existing), "\n") + "\n\n" + content
	}
	if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
		return err
	}
	fmt.Printf("%s: wrote gorefactor doctor rules\n", path)
	return nil
}

// renderDoctorRules generates the agent-context snippet from the live rule
// registry: structural rule names and the mechanically-fixable subset are
// enumerated from defaultLintRules(), the gate posture and substrate
// expectations are stated once.
func renderDoctorRules() string {
	var names, fixable []string
	for _, r := range defaultLintRules() {
		names = append(names, r.Name())
		if _, ok := r.(FixableRule); ok {
			fixable = append(fixable, r.Name())
		}
	}
	sort.Strings(fixable)

	var b strings.Builder
	b.WriteString(doctorRulesSentinel + "\n")
	b.WriteString("## gorefactor doctor: code health expectations\n\n")
	b.WriteString("This repo is checked by `gorefactor doctor --report` (structural lint + golangci-lint + temporal + modtidy, plus deadcode and govulncheck on full runs; the agent's doctor gate additionally runs apidiff). ")
	b.WriteString("Only **new** findings vs the base ref matter; error-severity categories (conc, sec, api, tmprl) gate, the rest report. Write code so findings never appear:\n\n")
	b.WriteString("- **Errors**: wrap with `fmt.Errorf(\"context: %w\", err)`; never log-and-return the same error; don't return bare shared sentinels.\n")
	b.WriteString("- **Tests**: every test must be able to fail (assert or pass `*testing.T` to a helper); never synchronize with `time.Sleep`.\n")
	b.WriteString("- **Libraries** (non-main packages): return errors instead of `log.Fatal`/`os.Exit`/`panic`; stop every `time.NewTicker`; give goroutines a visible lifecycle (context, done channel, or WaitGroup).\n")
	b.WriteString("- **Perf**: use `strings.Builder` for loop concatenation; hoist constant `regexp.MustCompile` to package level; prefer maps over repeated linear scans.\n")
	b.WriteString("- **Size/structure**: keep files under the size limit and functions short/flat; prefer extraction over deep nesting; don't forward a parameter through layers that never use it.\n")
	b.WriteString("- **API changes**: a deliberate change to the exported API must be declared first: `gorefactor intent api-change <scope> <reason>` — undeclared deltas fail the gate.\n")
	b.WriteString("- **Temporal workflows** (if the module uses go.temporal.io/sdk): workflow code must be deterministic — `workflow.Now`/`workflow.Sleep`/`workflow.Go`/`workflow.Selector`/`workflow.SideEffect`, never `time.Now`, `time.Sleep`, `go`, `select`, or `math/rand`.\n")
	b.WriteString("- **Dependencies**: keep go.mod tidy (`go mod tidy`).\n\n")
	fmt.Fprintf(&b, "Structural rules enforced (%d): %s.\n\n", len(names), strings.Join(names, ", "))
	fmt.Fprintf(&b, "Mechanically fixable via `gorefactor lint --fix --verify` (findings carry the exact command as a FixCmd): %s.\n\n", strings.Join(fixable, ", "))

	b.WriteString("Before finishing a change: run `gorefactor doctor --report --scoped` and clear any new findings it reports.\n")
	return b.String()
}
