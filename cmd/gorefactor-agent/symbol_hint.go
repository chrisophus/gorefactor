package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// argSym returns the symbol identifier the model targeted, checking the
// function/method/type args in order. Empty if none were supplied.
func argSym(a map[string]any) string {
	for _, k := range []string{"function", "method", "type"} {
		if s, ok := a[k].(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

// symbolDefFiles returns the non-test .go files that contain a top-level
// definition of name, scanning from the current module dir. The
// .gorefactor snapshot tree and vendor are skipped wholesale: snapshots
// are byte copies of real files, so counting them makes every
// snapshotted symbol look ambiguous and defeats resolveSymbolFile.
func symbolDefFiles(name string) []string {
	if name == "" {
		return nil
	}
	pats := []string{"func " + name + "(", ") " + name + "(", "type " + name + " "}
	seen := map[string]bool{}
	_ = filepath.WalkDir(".", func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if n := d.Name(); n == ".gorefactor" || n == "vendor" || n == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		b, e := os.ReadFile(p)
		if e != nil {
			return nil
		}
		s := string(b)
		for _, pat := range pats {
			if strings.Contains(s, pat) {
				seen[strings.TrimPrefix(p, "./")] = true
				break
			}
		}
		return nil
	})
	out := make([]string, 0, len(seen))
	for f := range seen {
		out = append(out, f)
	}
	return out
}

// resolveSymbolFile makes file-scoped symbol ops deterministic: the
// junior names the symbol reliably but guesses its file. If the given
// file does not define name but exactly one other file does, return
// that file (corrected=true) so the op targets the right place with no
// LLM retry. Ambiguous or not-found: caller falls back to a hint.
func resolveSymbolFile(name, given string) (resolved string, corrected bool) {
	files := symbolDefFiles(name)
	if len(files) == 0 {
		return given, false
	}
	for _, f := range files {
		if f == given {
			return given, false
		}
	}
	if len(files) == 1 {
		return files[0], true
	}
	return given, false
}

// symbolDefHint is the fallback when resolveSymbolFile cannot pick a
// single file (symbol defined in several places, or nowhere): name
// where it actually lives so the model can retry instead of punting.
func symbolDefHint(name, guessedFile string) string {
	var others []string
	for _, f := range symbolDefFiles(name) {
		if f != guessedFile {
			others = append(others, f)
		}
	}
	if len(others) == 0 {
		return ""
	}
	return fmt.Sprintf(" -- %s is defined in %s", name, strings.Join(others, ", "))
}
