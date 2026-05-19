package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/chrisophus/gorefactor/analyzer"
	"github.com/chrisophus/gorefactor/parser"
)

// --- sense tools (read-only, tight output per task #12) -------------

func senseListSymbols(file string) string {
	if file == "" {
		return "ERROR: 'file' required"
	}
	info, err := parser.ParseFile(file)
	if err != nil {
		return "ERROR: " + trim(err.Error(), 200)
	}
	var b strings.Builder
	for _, fn := range info.Functions {
		fmt.Fprintf(&b, "func %s\n", fn.Name)
	}
	for _, m := range info.Methods {
		fmt.Fprintf(&b, "method %s.%s\n", m.Receiver, m.Name)
	}
	return trim(b.String(), 1200)
}

func senseFileSize(file string) string {
	if file == "" {
		return "ERROR: 'file' required"
	}
	iss, err := analyzer.AnalyzeFileSize(file, 300)
	if err != nil {
		return "ERROR analyzing file size: " + err.Error()
	}
	if iss == nil {
		return "ERROR: no result returned for " + file
	}
	b := &strings.Builder{}
	fmt.Fprintf(b, "lines=%d limit=%d oversized=%v\n", iss.LineCount, iss.MaxRecommended, iss.IsOversized)
	for i, h := range iss.ExtractionHints {
		if i >= 6 {
			break
		}
		fmt.Fprintf(b, "hint: %s (lines %d-%d, complexity %d, priority %d)\n",
			h.FunctionName, h.StartLine, h.EndLine, h.Complexity, h.Priority)
	}
	return trim(b.String(), 1000)
}

func senseFindRefs(symbol string) string {
	if symbol == "" {
		return "ERROR: 'symbol' required"
	}
	var b strings.Builder
	n := 0
	for _, f := range goFiles(".") {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for i, line := range strings.Split(string(data), "\n") {
			if strings.Contains(line, symbol) {
				fmt.Fprintf(&b, "%s:%d\n", f, i+1)
				if n++; n >= 40 {
					b.WriteString("…(more)\n")
					return b.String()
				}
			}
		}
	}
	if n == 0 {
		return "no references found"
	}
	return b.String()
}
