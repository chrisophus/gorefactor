package main

import (
	"fmt"
	"os"
	"strings"
)

// readFileMaxLines bounds the whole-file read so a pathologically large file
// still cannot blow the context; above it the model is told to page with
// read_excerpt instead. Frontier context easily holds a normal source file,
// so reading once beats paging + re-reading (the multi-file thrash the small
// read_excerpt window used to cause).
const readFileMaxLines = 600

// senseReadFile returns an entire file with line numbers, so the junior can
// orient on a whole file in ONE tool call instead of paging read_excerpt and
// re-reading regions as they fall out of the masked history.
func senseReadFile(file string) string {
	if file == "" {
		return "ERROR: 'file' required"
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return "ERROR: " + trim(err.Error(), 200)
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) > readFileMaxLines {
		return fmt.Sprintf("ERROR: %s has %d lines (over the %d-line whole-file limit); use read_excerpt with start_line/end_line",
			file, len(lines), readFileMaxLines)
	}
	var b strings.Builder
	for i, ln := range lines {
		fmt.Fprintf(&b, "%d: %s\n", i+1, ln)
	}
	return b.String()
}
