package main

import (
	"fmt"
	"strings"
)

type lintOptions struct {
	root      string
	maxSize   int
	fix       bool
	jsonOut   bool
	onlyRules map[string]bool
	skipRules map[string]bool
	failOn    string // "error" | "warning"
}

func parseLintOptions(args []string) (lintOptions, error) {
	opts := lintOptions{
		root:      ".",
		maxSize:   defaultSplitMaxLines,
		failOn:    "error",
		onlyRules: make(map[string]bool),
		skipRules: make(map[string]bool),
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--fix":
			opts.fix = true
		case a == "--json":
			opts.jsonOut = true
		case a == "--max":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--max requires a value")
			}
			var n int
			if _, err := fmt.Sscanf(args[i+1], "%d", &n); err != nil || n <= 0 {
				return opts, fmt.Errorf("--max requires a positive integer")
			}
			opts.maxSize = n
			i++
		case a == "--rule":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--rule requires a value")
			}
			opts.onlyRules[args[i+1]] = true
			i++
		case a == "--skip-rule":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--skip-rule requires a value")
			}
			opts.skipRules[args[i+1]] = true
			i++
		case a == "--fail-on":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--fail-on requires error or warning")
			}
			switch args[i+1] {
			case "error", "warning":
				opts.failOn = args[i+1]
			default:
				return opts, fmt.Errorf("--fail-on must be error or warning")
			}
			i++
		case strings.HasPrefix(a, "--"):
			return opts, fmt.Errorf("unknown lint flag: %s", a)
		default:
			opts.root = a
		}
	}
	return opts, nil
}

func filterLintRules(all []LintRule, opts lintOptions) []LintRule {
	if len(opts.onlyRules) == 0 && len(opts.skipRules) == 0 {
		return all
	}
	var out []LintRule
	for _, r := range all {
		name := r.Name()
		if len(opts.onlyRules) > 0 && !opts.onlyRules[name] {
			continue
		}
		if opts.skipRules[name] {
			continue
		}
		out = append(out, r)
	}
	return out
}

func lintShouldFail(issues []lintIssue, failOn string) bool {
	for _, iss := range issues {
		if failOn == "warning" {
			return true
		}
		if iss.Severity == "error" {
			return true
		}
	}
	return false
}
