package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// docExemptCommands lists registered commands intentionally omitted from the
// CLAUDE.md command reference (e.g. internal-only entry points). Keep it empty
// unless a command is deliberately undocumented; every entry here is a hole in
// the doc-drift guarantee, so justify it in a comment.
var docExemptCommands = map[string]string{}

// TestDocDrift_CommandsAreDocumented is a sensor (not a guide): every command
// the CLI registers must be mentioned by name in the CLAUDE.md command
// reference, which is the primary interface LLMs read to drive gorefactor. A
// command added to getCommands() without a doc entry fails this test, keeping
// the hand-maintained table from silently drifting out of sync with the code.
func TestDocDrift_CommandsAreDocumented(t *testing.T) {
	doc := readClaudeMD(t)
	for _, name := range commandNames() {
		if reason, ok := docExemptCommands[name]; ok {
			t.Logf("skipping documented-exempt command %q: %s", name, reason)
			continue
		}
		if !containsCommandWord(doc, name) {
			t.Errorf("command %q is registered but not documented in CLAUDE.md; "+
				"add it to the command reference (or to docExemptCommands with a reason)", name)
		}
	}
}

// readClaudeMD loads the repo-root CLAUDE.md relative to this test's package
// directory (cmd/gorefactor -> ../../CLAUDE.md).
func readClaudeMD(t *testing.T) string {
	t.Helper()
	path := filepath.Join("..", "..", "CLAUDE.md")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}
	return string(b)
}

// containsCommandWord reports whether word occurs in s delimited by characters
// that are not part of a command identifier ([a-zA-Z0-9-]). Hyphenated command
// names (change-signature, remove-log-return) are treated as single tokens, so
// a prefix like "change" or "replace" cannot match a longer command and "split"
// does not match "split-file".
func containsCommandWord(s, word string) bool {
	from := 0
	for {
		i := strings.Index(s[from:], word)
		if i < 0 {
			return false
		}
		i += from
		if !isIdentByte(byteAt(s, i-1)) && !isIdentByte(byteAt(s, i+len(word))) {
			return true
		}
		from = i + 1
	}
}

func byteAt(s string, i int) byte {
	if i < 0 || i >= len(s) {
		return ' '
	}
	return s[i]
}

func isIdentByte(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9', b == '-':
		return true
	default:
		return false
	}
}
