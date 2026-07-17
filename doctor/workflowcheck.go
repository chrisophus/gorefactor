package doctor

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// runWorkflowcheck runs Temporal's official workflowcheck analyzer when it is
// on PATH — the reuse-first engine for the temporal substrate. ok=false means
// the binary is absent and the caller should fall back to the in-process
// scan.
func runWorkflowcheck(ctx RunContext) (findings []Finding, ok bool, err error) {
	if _, lerr := exec.LookPath("workflowcheck"); lerr != nil {
		return nil, false, nil
	}
	args := []string{}
	if len(ctx.ScopeDirs) == 0 {
		args = append(args, "./...")
	} else {
		for _, d := range ctx.ScopeDirs {
			args = append(args, "./"+filepath.ToSlash(d))
		}
	}
	cmd := exec.Command("workflowcheck", args...)
	cmd.Dir = ctx.Root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, runErr := cmd.Output()
	// workflowcheck exits non-zero when it reports diagnostics; only an empty
	// report alongside an error is a real failure. Diagnostics go to stderr in
	// go/analysis drivers, so parse both streams.
	combined := append(out, stderr.Bytes()...)
	parsed := parseWorkflowcheckOutput(combined, ctx.Root)
	if runErr != nil && len(parsed) == 0 {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = runErr.Error()
		}
		return nil, true, unavailablef("workflowcheck failed to run: %s", msg)
	}
	return parsed, true, nil
}

// workflowcheckLine matches go/analysis diagnostic lines: file:line[:col]: message.
var workflowcheckLine = regexp.MustCompile(`^(.+\.go):(\d+)(?::\d+)?: (.+)$`)

// parseWorkflowcheckOutput maps workflowcheck diagnostic lines to findings.
func parseWorkflowcheckOutput(out []byte, root string) []Finding {
	var findings []Finding
	for _, line := range strings.Split(string(out), "\n") {
		m := workflowcheckLine.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		lineNo, err := strconv.Atoi(m[2])
		if err != nil {
			continue
		}
		file := m[1]
		if filepath.IsAbs(file) {
			if rel, rerr := filepath.Rel(root, file); rerr == nil {
				file = rel
			}
		}
		findings = append(findings, Finding{
			File:     filepath.ToSlash(file),
			Line:     lineNo,
			Rule:     "temporal/workflowcheck",
			Category: CategoryTemporal,
			Message:  m[3],
		})
	}
	return findings
}
