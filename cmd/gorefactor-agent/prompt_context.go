package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/chrisophus/gorefactor/parser"
)

// fileList returns a compact newline-separated list of non-test Go file paths
// relative to dir. Used in the system prompt so the model knows valid paths
// without the token cost of a full symbol dump.
func fileList(dir string) string {
	files := goFiles(dir)
	var b strings.Builder
	for _, f := range files {
		rel, _ := filepath.Rel(dir, f)
		b.WriteString(rel)
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

func goFiles(dir string) []string {
	var files []string
	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			if d != nil && d.IsDir() {
				n := d.Name()
				// Skip VCS/deps/test fixtures and ALL dot-dirs — in
				// particular .gorefactor (gorefactor's own snapshot/
				// undo store). Targeting snapshot copies would make the
				// campaign chase its own artifacts and never converge.
				if n == "vendor" || n == "testdata" ||
					(len(n) > 1 && n[0] == '.') {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			files = append(files, path)
		}
		return nil
	})
	sort.Strings(files)
	return files
}

func codeMap(dir string) string {
	var b strings.Builder
	files := goFiles(dir)

	for _, f := range files {
		rel, _ := filepath.Rel(dir, f)
		info, err := parser.ParseFile(f)
		if err != nil {
			continue
		}
		b.WriteString(rel)
		b.WriteString(":\n")
		for _, fn := range info.Functions {
			fmt.Fprintf(&b, "  func %s\n", fn.Name)
		}
		for _, m := range info.Methods {
			fmt.Fprintf(&b, "  method %s.%s\n", m.Receiver, m.Name)
		}
	}
	if b.Len() == 0 {
		return "(no Go files found)"
	}
	return b.String()
}

// specTokens pulls candidate identifiers/words out of a spec for
// matching against code. Deterministic, no NLP -- just enough signal
// to rank files.
func specTokens(spec string) []string {
	seen := map[string]bool{}
	var out []string
	cur := strings.Builder{}
	flush := func() {
		if cur.Len() == 0 {
			return
		}
		t := strings.ToLower(cur.String())
		cur.Reset()
		if len(t) < 3 || specStopwords[t] || seen[t] {
			return
		}
		seen[t] = true
		out = append(out, t)
	}
	for _, r := range spec {
		if r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			cur.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return out
}

// relevantSource is deterministic feedforward context: it ranks files
// by how well their path and symbol names match the spec, then inlines
// the actual source of the top matches within a byte budget. A cheap
// model can't target or write fitting code from a symbol map alone.
func relevantSource(spec, dir string, totalBudget, perFileCap int) string {
	tokens := specTokens(spec)
	if len(tokens) == 0 {
		return "(spec has no distinctive terms; rely on the code map)"
	}
	ranked := rankFilesBySpec(dir, tokens)
	if len(ranked) == 0 {
		return "(no files matched the spec terms; rely on the code map)"
	}
	return inlineRankedSources(ranked, totalBudget, perFileCap)

}

type specScoredFile struct {
	rel   string
	path  string
	score int
}

func scoreSpecFileMatch(f, rel string, tokens []string) int {
	relLower := strings.ToLower(rel)
	score := 0
	for _, t := range tokens {
		if strings.Contains(relLower, t) {
			score += 2
		}
	}
	if info, err := parser.ParseFile(f); err == nil {
		names := make([]string, 0, len(info.Functions)+len(info.Methods))
		for _, fn := range info.Functions {
			names = append(names, fn.Name)
		}
		for _, m := range info.Methods {
			names = append(names, m.Name)
		}
		for _, n := range names {
			nl := strings.ToLower(n)
			for _, t := range tokens {
				if strings.Contains(nl, t) || strings.Contains(t, nl) {
					score += 3
				}
			}
		}
	}
	if data, err := os.ReadFile(f); err == nil {
		cl := strings.ToLower(string(data))
		for _, t := range tokens {
			if c := strings.Count(cl, t); c > 0 {
				score += min(c, 3)
			}
		}
	}
	return score
}

func rankFilesBySpec(dir string, tokens []string) []specScoredFile {
	var ranked []specScoredFile
	for _, f := range goFiles(dir) {
		rel, _ := filepath.Rel(dir, f)
		if score := scoreSpecFileMatch(f, rel, tokens); score > 0 {
			ranked = append(ranked, specScoredFile{rel, f, score})
		}
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].rel < ranked[j].rel
	})
	return ranked
}

func inlineRankedSources(ranked []specScoredFile, totalBudget, perFileCap int) string {
	var b strings.Builder
	used := 0
	for _, s := range ranked {
		if used >= totalBudget {
			break
		}
		data, err := os.ReadFile(s.path)
		if err != nil {
			continue
		}
		src := string(data)
		truncated := false
		if len(src) > perFileCap {
			src = src[:perFileCap]
			truncated = true
		}
		fmt.Fprintf(&b, "=== %s ===\n%s\n", s.rel, src)
		if truncated {
			b.WriteString("…(file truncated)\n")
		}
		used += len(src)
	}
	return strings.TrimRight(b.String(), "\n")
}
