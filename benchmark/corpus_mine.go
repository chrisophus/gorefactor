package main

// corpus_mine.go: Tier-1 Part A of the harness feedback loop — graduate
// recurring entries in .gorefactor/failures.jsonl into eval-corpus task
// stubs. The failure corpus is write-only until here; this is the
// sensor->eval bridge that makes a mistake-cannot-recur (Hashimoto): a
// pattern that failed >=N times becomes a permanent regression task, and
// once its capability ships that task must flip to `efficient`.
//
// It lives in the benchmark package (next to agentTask + the outcome
// constants it emits) rather than in the agent binary, and it never
// mutates the compiled corpus (agent_tasks.go): it prints clusters and,
// with -emit-tasks, writes reviewable Go stubs to a scratch file for a
// human to paste in. Human-in-the-loop by design.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// failureCorpusRelPath mirrors corpusRelPath in the agent package; the
// two binaries share the on-disk contract, not a Go type.
const failureCorpusRelPath = ".gorefactor/failures.jsonl"

// Failure-corpus record kinds (mirror of the agent package's fail* consts).
const (
	failRejectedOp    = "rejected_op"
	failBudgetHit     = "budget_hit"
	failPunt          = "punt"
	failCapabilityGap = "capability_gap"
)

// minedFailure is the read-side view of one corpus line. It intentionally
// re-declares the fields (rather than importing the agent's failureEntry)
// so the benchmark tool depends only on the JSONL contract.
type minedFailure struct {
	TS      string `json:"ts"`
	Kind    string `json:"kind"`
	Tool    string `json:"tool,omitempty"`
	Op      string `json:"op,omitempty"`
	Reason  string `json:"reason"`
	Spec    string `json:"spec,omitempty"`
	Context string `json:"context,omitempty"`
}

// failureCluster groups corpus entries that share a (kind, tool,
// normalized-reason) signature. Count is the recurrence signal; only
// clusters at or above the threshold graduate.
type failureCluster struct {
	Kind      string
	Tool      string
	ReasonKey string // normalized reason (grouping key)
	Count     int
	Specs     []string       // distinct originating specs (seed material)
	Examples  []minedFailure // raw entries, first-seen order
}

// readFailures loads every parseable line of the corpus under dir.
// Malformed lines are skipped, not fatal: the corpus is append-only and
// best-effort, so a torn write must not sink the whole mine pass.
func readFailures(dir string) ([]minedFailure, error) {
	path := filepath.Join(dir, failureCorpusRelPath)
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []minedFailure
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e minedFailure
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		out = append(out, e)
	}
	return out, sc.Err()
}

var (
	reQuoted = regexp.MustCompile("\"[^\"]*\"|'[^']*'|`[^`]*`")
	rePath   = regexp.MustCompile(`[\w./-]+\.go(:\d+)?`)
	reNum    = regexp.MustCompile(`\b\d+\b`)
	reSpace  = regexp.MustCompile(`\s+`)
)

// normalizeReason strips the variable parts of a failure reason (quoted
// literals, file paths, line/column numbers) so that the same underlying
// defect clusters together regardless of which symbol or file tripped it.
func normalizeReason(s string) string {
	s = strings.ToLower(s)
	s = reQuoted.ReplaceAllString(s, "<q>")
	s = rePath.ReplaceAllString(s, "<path>")
	s = reNum.ReplaceAllString(s, "<n>")
	s = reSpace.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// clusterFailures groups entries by (kind, tool, normalized-reason) and
// returns clusters sorted by descending count (ties broken by kind/tool
// for stable output).
func clusterFailures(entries []minedFailure) []failureCluster {
	type key struct{ kind, tool, reason string }
	index := map[key]*failureCluster{}
	var order []*failureCluster
	seenSpec := map[key]map[string]bool{}

	for _, e := range entries {
		k := key{e.Kind, e.Tool, normalizeReason(e.Reason)}
		c := index[k]
		if c == nil {
			c = &failureCluster{Kind: e.Kind, Tool: e.Tool, ReasonKey: k.reason}
			index[k] = c
			seenSpec[k] = map[string]bool{}
			order = append(order, c)
		}
		c.Count++
		c.Examples = append(c.Examples, e)
		if e.Spec != "" && !seenSpec[k][e.Spec] {
			seenSpec[k][e.Spec] = true
			c.Specs = append(c.Specs, e.Spec)
		}
	}

	out := make([]failureCluster, 0, len(order))
	for _, c := range order {
		out = append(out, *c)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Tool < out[j].Tool
	})
	return out
}

// diagnoseCluster maps a cluster to one of the plan's failure classes plus
// the expected post-fix outcome for the emitted task. The diagnosis is
// advisory — it seeds the human reviewer, who confirms before commit.
func diagnoseCluster(c failureCluster) (class string, expected expectedOutcome) {
	switch c.Kind {
	case failCapabilityGap:
		// A command the agent needed did not exist. Once built, the task
		// must complete cleanly.
		return "missing-capability", outEfficient
	case failRejectedOp:
		// The tool existed but refused the op: either the agent formed it
		// wrong (routing/format) or a guardrail correctly blocked it. The
		// reviewer disambiguates; default expectation is a clean pass once
		// the routing/format is taught.
		return "routing-or-guardrail", outEfficient
	case failBudgetHit:
		return "efficiency-regression", outEfficient
	case failPunt:
		// A punt is a judgment/gap handback; until diagnosed it stays a
		// friction case rather than asserting a clean pass.
		return "punt-judgment-gap", outFriction
	default:
		return "unknown", outFriction
	}
}

var reNonIdent = regexp.MustCompile(`[^a-zA-Z0-9]+`)

// stubID builds a stable, readable task id from a cluster's signature.
func stubID(c failureCluster, n int) string {
	tool := firstNonEmpty(c.Tool, c.Kind)
	slug := strings.Trim(reNonIdent.ReplaceAllString(strings.ToLower(tool), "-"), "-")
	return fmt.Sprintf("mined-%s-%d", slug, n)
}

// emitTaskStub renders one cluster as a compilable agentTask literal in
// the agent_tasks.go shape. Fixture is left as a TODO because a failure
// entry does not carry the source it ran against; the human supplies a
// minimal reproducer. Everything else is seeded from the cluster so the
// reviewer starts from real signal, not a blank template.
func emitTaskStub(c failureCluster, n int) string {
	class, expected := diagnoseCluster(c)
	spec := "TODO: describe the task (seed below)"
	if len(c.Specs) > 0 {
		spec = c.Specs[0]
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\t\t// mined from %d %q failure(s); class=%s; reason=%q\n",
		c.Count, c.Kind, class, trimTo(c.ReasonKey, 120))
	b.WriteString("\t\t{\n")
	fmt.Fprintf(&b, "\t\t\tID: %q, Difficulty: %q, Probes: %q,\n", stubID(c, n), "medium", firstNonEmpty(c.Tool, c.Kind))
	fmt.Fprintf(&b, "\t\t\tExpected: %s,\n", outcomeConst(expected))
	fmt.Fprintf(&b, "\t\t\tSpec: %q,\n", trimTo(spec, 300))
	b.WriteString("\t\t\tFixture: map[string]string{\n")
	b.WriteString("\t\t\t\t\"go.mod\": gomod,\n")
	b.WriteString("\t\t\t\t// TODO: minimal reproducer file(s) that trigger the above\n")
	b.WriteString("\t\t\t},\n")
	b.WriteString("\t\t\t// TODO: add Assert []oracleCheck once the intended transform is pinned down\n")
	b.WriteString("\t\t},\n")
	return b.String()
}

// outcomeConst renders an expectedOutcome as its Go constant identifier so
// the emitted stub references the symbol, not a bare string literal.
func outcomeConst(o expectedOutcome) string {
	switch o {
	case outEfficient:
		return "outEfficient"
	case outFriction:
		return "outFriction"
	case outFail:
		return "outFail"
	default:
		return "outFriction"
	}
}

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if x != "" {
			return x
		}
	}
	return ""
}

// trimTo shortens s to max runes with an ellipsis (local to benchmark).
func trimTo(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

// runMineFailures reads the corpus under dir, clusters it, prints clusters
// at or above minCount, and (when emitTasks) writes their task stubs to a
// scratch file it reports on stdout. Returns the number of graduating
// clusters.
func runMineFailures(dir string, minCount int, emitTasks bool, out io.Writer) (int, error) {
	entries, err := readFailures(dir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(out, "no failure corpus at %s (nothing to mine)\n", filepath.Join(dir, failureCorpusRelPath))
			return 0, nil
		}
		return 0, err
	}
	clusters := clusterFailures(entries)

	var graduating []failureCluster
	for _, c := range clusters {
		if c.Count >= minCount {
			graduating = append(graduating, c)
		}
	}

	fmt.Fprintf(out, "%d failure entries, %d clusters, %d at/above min-count=%d\n",
		len(entries), len(clusters), len(graduating), minCount)
	for _, c := range graduating {
		class, _ := diagnoseCluster(c)
		fmt.Fprintf(out, "  [%dx] kind=%s tool=%s class=%s reason=%q\n",
			c.Count, c.Kind, firstNonEmpty(c.Tool, "-"), class, trimTo(c.ReasonKey, 80))
	}

	if emitTasks && len(graduating) > 0 {
		var src strings.Builder
		src.WriteString("// Mined task stubs — REVIEW before pasting into benchmark/agent_tasks.go.\n")
		src.WriteString("// Each needs a Fixture (minimal reproducer) and ideally an Assert oracle.\n\n")
		for i, c := range graduating {
			src.WriteString(emitTaskStub(c, i+1))
			src.WriteString("\n")
		}
		stubPath := filepath.Join(dir, ".gorefactor", "mined_tasks.go.txt")
		if err := os.MkdirAll(filepath.Dir(stubPath), 0o755); err != nil {
			return len(graduating), fmt.Errorf("creating .gorefactor dir: %w", err)
		}
		if err := os.WriteFile(stubPath, []byte(src.String()), 0o644); err != nil {
			return len(graduating), fmt.Errorf("writing task stubs: %w", err)
		}
		fmt.Fprintf(out, "wrote %d task stub(s) to %s (review, then paste into benchmark/agent_tasks.go)\n",
			len(graduating), stubPath)
	}
	return len(graduating), nil
}
