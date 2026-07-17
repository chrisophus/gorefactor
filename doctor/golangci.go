package doctor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Golangci is the breadth-layer substrate: it shells out to golangci-lint and
// maps its JSON issues into findings. Semantics mirror doctor's existing
// golangci stage: "could not run at all" (missing/mismatched binary, config
// load error) is ErrUnavailable, distinct from "ran and found N issues".
type Golangci struct{}

// Info implements Substrate.
func (Golangci) Info() SubstrateInfo {
	return SubstrateInfo{Name: "golangci", Gating: true, ScopeCapable: true}
}

// Run implements Substrate.
func (Golangci) Run(ctx RunContext) ([]Finding, error) {
	if _, err := exec.LookPath("golangci-lint"); err != nil {
		return nil, unavailablef("golangci-lint not on PATH")
	}
	if !hasGolangciConfig(ctx.Root) {
		return nil, unavailablef("no .golangci config in %s", ctx.Root)
	}
	args := []string{"run", "--output.json.path", "stdout", "--output.text.path", "/dev/null"}
	if len(ctx.ScopeDirs) == 0 {
		args = append(args, "./...")
	} else {
		for _, d := range ctx.ScopeDirs {
			args = append(args, "./"+filepath.ToSlash(d))
		}
	}
	cmd := exec.Command("golangci-lint", args...)
	cmd.Dir = ctx.Root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, runErr := cmd.Output()
	if len(out) == 0 {
		if runErr != nil {
			msg := strings.TrimSpace(stderr.String())
			if msg == "" {
				msg = runErr.Error()
			}
			return nil, unavailablef("golangci-lint failed to run: %s", msg)
		}
		return nil, nil
	}
	return parseGolangciJSON(out)
}

func hasGolangciConfig(root string) bool {
	for _, name := range []string{".golangci.yml", ".golangci.yaml", ".golangci.toml", ".golangci.json"} {
		if _, err := os.Stat(filepath.Join(root, name)); err == nil {
			return true
		}
	}
	return false
}

// parseGolangciJSON maps a golangci-lint JSON report to findings. Severity is category-derived
// (plan decision 3b); golangci's own severities are ignored. Only the first JSON value is decoded:
// golangci v2 can append a text stats line after the JSON object even with the text writer pointed
// elsewhere.
func parseGolangciJSON(out []byte) ([]Finding, error) {
	var report struct {
		Issues []struct {
			FromLinter string `json:"FromLinter"`
			Text       string `json:"Text"`
			Pos        struct {
				Filename string `json:"Filename"`
				Line     int    `json:"Line"`
			} `json:"Pos"`
		} `json:"Issues"`
	}
	if err := json.NewDecoder(bytes.NewReader(out)).Decode(&report); err != nil {
		return nil, fmt.Errorf("parse golangci-lint JSON: %w", err)
	}
	var findings []Finding
	for _, iss := range report.Issues {
		findings = append(findings, Finding{
			File:     iss.Pos.Filename,
			Line:     iss.Pos.Line,
			Rule:     "golangci/" + iss.FromLinter,
			Category: golangciCategory(iss.FromLinter),
			Message:  iss.Text,
		})
	}
	return findings, nil
}

// golangciCategory maps a golangci linter name to a doctor category. Anything
// unmapped lands in the warning-severity lint category rather than guessing an
// error-severity one.
func golangciCategory(linter string) Category {
	switch linter {
	case "gosec":
		return CategorySec
	case "unused", "deadcode", "unparam", "wastedassign":
		return CategoryDead
	case "prealloc", "perfsprint", "makezero":
		return CategoryPerf
	default:
		return CategoryLint
	}
}
