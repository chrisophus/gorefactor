package main

import (
	"fmt"
	"strings"
)

// unifiedDiff renders a unified diff (context 3) between before and after
// for the given path. Returns "" when the contents are identical.
func unifiedDiff(path, before, after string) string {
	if before == after {
		return ""
	}
	a := splitLines(before)
	b := splitLines(after)
	ops := diffOps(a, b)

	var out strings.Builder
	fmt.Fprintf(&out, "--- a/%s\n+++ b/%s\n", path, path)

	const context = 3
	i := 0
	for i < len(ops) {
		// Skip runs of equal lines until the next change.
		if ops[i].kind == ' ' {
			i++
			continue
		}
		// Hunk starts up to `context` equal lines before the change.
		start := i
		for start > 0 && ops[start-1].kind == ' ' && i-start < context {
			start--
		}
		// Extend the hunk through changes separated by <= 2*context equals.
		end := i
		for j := i; j < len(ops); j++ {
			if ops[j].kind != ' ' {
				end = j + 1
			} else if j-end >= 2*context {
				break
			}
		}
		stop := end
		for stop < len(ops) && ops[stop].kind == ' ' && stop-end < context {
			stop++
		}

		aStart, bStart := ops[start].aIdx+1, ops[start].bIdx+1
		aCount, bCount := 0, 0
		for _, op := range ops[start:stop] {
			if op.kind != '+' {
				aCount++
			}
			if op.kind != '-' {
				bCount++
			}
		}
		fmt.Fprintf(&out, "@@ -%d,%d +%d,%d @@\n", aStart, aCount, bStart, bCount)
		for _, op := range ops[start:stop] {
			out.WriteByte(op.kind)
			out.WriteString(op.text)
			out.WriteByte('\n')
		}
		i = stop
	}
	return out.String()
}

// diffLineCount returns the number of added plus removed lines.
func diffLineCount(before, after string) int {
	if before == after {
		return 0
	}
	n := 0
	for _, op := range diffOps(splitLines(before), splitLines(after)) {
		if op.kind != ' ' {
			n++
		}
	}
	return n
}

type diffOp struct {
	kind byte // ' ', '-', '+'
	text string
	aIdx int
	bIdx int
}

// diffOps computes a line-level diff via LCS.
func diffOps(a, b []string) []diffOp {
	// lcs[i][j] = length of LCS of a[i:], b[j:]
	lcs := make([][]int, len(a)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	var ops []diffOp
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			ops = append(ops, diffOp{' ', a[i], i, j})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			ops = append(ops, diffOp{'-', a[i], i, j})
			i++
		default:
			ops = append(ops, diffOp{'+', b[j], i, j})
			j++
		}
	}
	for ; i < len(a); i++ {
		ops = append(ops, diffOp{'-', a[i], i, j})
	}
	for ; j < len(b); j++ {
		ops = append(ops, diffOp{'+', b[j], i, j})
	}
	return ops
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	s = strings.TrimSuffix(s, "\n")
	return strings.Split(s, "\n")
}
