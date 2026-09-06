package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/chrisophus/gorefactor/version"
)

// SARIF (Static Analysis Results Interchange Format, OASIS 2.1.0) output for
// the lint command. gorefactor already emits its own JSON envelope via --json;
// --sarif emits the standard shape so GitHub code scanning and any other SARIF
// consumer ingest findings without a bespoke adapter. Both flags read the same
// filtered issue list, so --fail-only and --info shape SARIF identically.

const (
	sarifSchemaURI    = "https://json.schemastore.org/sarif-2.1.0.json"
	sarifVersion      = "2.1.0"
	gorefactorInfoURI = "https://github.com/chrisophus/gorefactor"
)

type sarifLog struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name           string          `json:"name"`
	InformationURI string          `json:"informationUri"`
	Version        string          `json:"version"`
	Rules          []sarifRuleDesc `json:"rules"`
}

type sarifRuleDesc struct {
	ID string `json:"id"`
}

type sarifResult struct {
	RuleID    string          `json:"ruleId"`
	Level     string          `json:"level"`
	Message   sarifMessage    `json:"message"`
	Locations []sarifLocation `json:"locations"`
}

type sarifMessage struct {
	Text string `json:"text"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysicalLocation `json:"physicalLocation"`
}

type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifactLocation `json:"artifactLocation"`
	Region           *sarifRegion          `json:"region,omitempty"`
}

type sarifArtifactLocation struct {
	URI string `json:"uri"`
}

type sarifRegion struct {
	StartLine   int `json:"startLine"`
	StartColumn int `json:"startColumn,omitempty"`
}

// sarifLevel maps gorefactor severities to the SARIF result-level enum. SARIF
// has no "info"; advisory findings map to "note".
func sarifLevel(severity string) string {
	switch severity {
	case "error":
		return "error"
	case "warning":
		return "warning"
	default:
		return "note"
	}
}

// splitSarifLocation splits a lintIssue.File ("path", "path:line", or
// "path:line:col") into a path and optional line/column. A trailing segment
// counts as a number only when it is all digits, so a path that itself contains
// a colon is left intact. Whole-file and whole-directory findings carry no
// line and get no region.
func splitSarifLocation(field string) (path string, line, col int) {
	path = field
	i := strings.LastIndexByte(path, ':')
	if i < 0 {
		return path, 0, 0
	}
	n, err := strconv.Atoi(path[i+1:])
	if err != nil {
		return path, 0, 0
	}
	rest := path[:i]
	if j := strings.LastIndexByte(rest, ':'); j >= 0 {
		if m, err := strconv.Atoi(rest[j+1:]); err == nil {
			return rest[:j], m, n
		}
	}
	return rest, n, 0
}

// buildSarifLog assembles the SARIF document from the filtered issue list. The
// driver advertises the distinct rules seen, in first-appearance order, so a
// consumer can enumerate them without a second pass.
func buildSarifLog(issues []lintIssue) sarifLog {
	results := make([]sarifResult, 0, len(issues))
	seen := map[string]struct{}{}
	rules := make([]sarifRuleDesc, 0)
	for _, iss := range issues {
		if _, ok := seen[iss.Rule]; !ok {
			seen[iss.Rule] = struct{}{}
			rules = append(rules, sarifRuleDesc{ID: iss.Rule})
		}
		path, line, col := splitSarifLocation(iss.File)
		var region *sarifRegion
		if line > 0 {
			region = &sarifRegion{StartLine: line, StartColumn: col}
		}
		results = append(results, sarifResult{
			RuleID:  iss.Rule,
			Level:   sarifLevel(iss.Severity),
			Message: sarifMessage{Text: iss.Message},
			Locations: []sarifLocation{{
				PhysicalLocation: sarifPhysicalLocation{
					ArtifactLocation: sarifArtifactLocation{URI: path},
					Region:           region,
				},
			}},
		})
	}
	return sarifLog{
		Schema:  sarifSchemaURI,
		Version: sarifVersion,
		Runs: []sarifRun{{
			Tool: sarifTool{Driver: sarifDriver{
				Name:           "gorefactor",
				InformationURI: gorefactorInfoURI,
				Version:        version.Version(),
				Rules:          rules,
			}},
			Results: results,
		}},
	}
}

func lintOutputSARIF(outputIssues, issues []lintIssue, opts lintOptions, shouldFail bool) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(buildSarifLog(outputIssues)); err != nil {
		return err
	}
	if shouldFail {
		return fmt.Errorf(
			"lint: %d issue(s) at or above %s severity (%d total issue(s))",
			failingIssueCount(issues, opts.failOn),
			opts.failOn,
			len(issues),
		)
	}
	return nil
}
