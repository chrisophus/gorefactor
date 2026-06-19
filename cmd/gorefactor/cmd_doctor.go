package main

import (
	"strings"
)

func init() {
	registerCommand(Command{
		Name:        "doctor",
		Description: "Aggregate health gate: lint + build + test. Exits non-zero on failure. [--json]",
		Usage:       "doctor [dir] [--json]",
		MinArgs:     0,
		MaxArgs:     1,
		Flags:       map[string]bool{"--json": false},
		Run:         doctorCommand,
	})
}

// 1. structural lint

// 2. build

// 3. test

func trimOutput(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	if s == "" {
		return "ok"
	}
	return s
}
