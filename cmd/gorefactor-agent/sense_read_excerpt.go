package main

import (
	"fmt"
	"os"
	"strings"
)

func senseReadExcerpt(file string, a map[string]any) string {
	if file == "" {
		return "ERROR: 'file' required"
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return "ERROR: " + trim(err.Error(), 200)
	}
	lines := strings.Split(string(data), "\n")
	num := func(k string, def int) int {
		if f, ok := a[k].(float64); ok {
			return int(f)
		}
		return def
	}
	start := num("start_line", 1)
	end := num("end_line", start+120)
	if start < 1 {
		start = 1
	}
	if end > len(lines) {
		end = len(lines)
	}
	// Bounded window. The original 80-line cap was a small-context
	// local-model assumption; a frontier junior can hold a larger view,
	// and paging a file 5 times to re-orient costs more input tokens than
	// one wider read. Still bounded so a huge file is not dumped whole.
	if end-start > 160 {
		end = start + 160
	}
	if start > end {
		return "ERROR: start_line > end_line"
	}
	var b strings.Builder
	for i := start; i <= end; i++ {
		fmt.Fprintf(&b, "%d: %s\n", i, lines[i-1])
	}
	return trim(b.String(), 6000)
}
