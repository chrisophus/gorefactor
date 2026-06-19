package main

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

var txnFlags = map[string]bool{"--json": false, "--gate": false}

func init() {
	registerCommand(Command{
		Name:        "txn",
		Description: "Apply a batch of mutation commands transactionally (all-or-nothing, single undo unit)",
		Usage:       "txn [file|-] [--json] [--gate]",
		MinArgs:     0,
		MaxArgs:     1,
		Flags:       txnFlags,
		Run:         txnCommand,
	})
}

// txnAllowedCommands are the mutation commands routed through the shared
// mutation runner — the only ones whose effects the transaction can capture
// and roll back. Read-only commands are excluded on purpose: a txn script is
// a write batch.
var txnAllowedCommands = map[string]bool{
	"create":          true,
	"insert":          true,
	"replace":         true,
	"replace-text":    true,
	"replace-body":    true,
	"set-doc":         true,
	"add-field":       true,
	"inline":          true,
	"change-receiver": true,
	"delete":          true,
	"rename":          true,
	"move":            true,
	"extract":         true,
	"format":          true,
	"split":           true,
}

// txnCollector accumulates the union of pre-mutation file states across all
// operations of a transaction. The first recorded state of each path wins —
// that is the state the whole transaction restores to.
type txnCollector struct {
	before  map[string][]byte
	created map[string]bool
	seen    map[string]bool
}

// activeTxn, when non-nil, redirects mutation-runner journaling into the
// collector (see mutation.run).
var activeTxn *txnCollector

func newTxnCollector() *txnCollector {
	return &txnCollector{
		before:  map[string][]byte{},
		created: map[string]bool{},
		seen:    map[string]bool{},
	}
}

func (c *txnCollector) record(before map[string][]byte, created []string) {
	for path, content := range before {
		if !c.seen[path] {
			c.seen[path] = true
			c.before[path] = content
		}
	}
	for _, path := range created {
		if !c.seen[path] {
			c.seen[path] = true
			c.created[path] = true
		}
	}
}

// restore puts every touched file back to its pre-transaction state.
func (c *txnCollector) restore() {
	for path, content := range c.before {
		_ = os.WriteFile(path, content, 0644)
	}
	for path := range c.created {
		_ = os.Remove(path)
	}
}

// touched returns all paths the transaction modified or created, sorted.
func (c *txnCollector) touched() []string {
	var paths []string
	for p := range c.seen {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths
}

// txnOpResult is the per-line result in --json output.
type txnOpResult struct {
	Line    int    `json:"line"`
	Command string `json:"command"`
	Args    string `json:"args,omitempty"`
	Success bool   `json:"success"`
	Output  string `json:"output,omitempty"`
	Error   string `json:"error,omitempty"`
}

// txnResult is the overall --json result of a transaction.
type txnResult struct {
	Success      bool          `json:"success"`
	Operation    string        `json:"operation"`
	Ops          []txnOpResult `json:"ops"`
	FilesChanged []string      `json:"filesChanged,omitempty"`
	UndoToken    string        `json:"undoToken,omitempty"`
	Error        string        `json:"error,omitempty"`
}

// Parse gate over every touched .go file.

// deleted by a later op

// Commit: one journal entry for the whole batch.

// txnFail renders a failed transaction for --json consumers and returns the
// error (with its semantic exit code) unchanged.
func txnFail(jsonOut bool, ops []txnOpResult, collector *txnCollector, err error) error {
	if jsonOut {
		res := txnResult{Success: false, Operation: "txn", Ops: ops, Error: err.Error()}
		if collector != nil {
			res.FilesChanged = nil // everything was rolled back
		}
		emitJSON(res)
	}
	return err
}

func txnCommandList() []string {
	var names []string
	for n := range txnAllowedCommands {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// readTxnScript reads the script from the positional file argument or stdin.
func readTxnScript(pos []string) (string, error) {
	if len(pos) >= 1 && pos[0] != "-" {
		b, err := os.ReadFile(pos[0])
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	b, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

type txnLine struct {
	line int
	argv []string
}

// parseTxnScript splits the script into command lines, skipping blanks and
// `#` comments. Each line uses CLI syntax minus the program name.
func parseTxnScript(script string) ([]txnLine, error) {
	var out []txnLine
	for i, raw := range strings.Split(script, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		argv, err := splitCommandLine(line)
		if err != nil {
			return nil, usageErrorf("txn: line %d: %v", i+1, err)
		}
		if len(argv) == 0 {
			continue
		}
		out = append(out, txnLine{line: i + 1, argv: argv})
	}
	return out, nil
}

// splitCommandLine tokenizes a command line with shell-like quoting: single
// and double quotes group words; backticks are ordinary characters (struct
// tags); backslash escapes the next character outside single quotes.
func splitCommandLine(s string) ([]string, error) {
	var argv []string
	var cur strings.Builder
	inWord := false
	quote := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote == '\'':
			if c == '\'' {
				quote = 0
			} else {
				cur.WriteByte(c)
			}
		case quote == '"':
			if c == '"' {
				quote = 0
			} else if c == '\\' && i+1 < len(s) {
				i++
				cur.WriteByte(s[i])
			} else {
				cur.WriteByte(c)
			}
		case c == '\'' || c == '"':
			quote = c
			inWord = true
		case c == '\\' && i+1 < len(s):
			i++
			cur.WriteByte(s[i])
			inWord = true
		case c == ' ' || c == '\t':
			if inWord {
				argv = append(argv, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteByte(c)
			inWord = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated %c-quoted string", quote)
	}
	if inWord {
		argv = append(argv, cur.String())
	}
	return argv, nil
}

// captureStdoutOf runs fn with os.Stdout redirected to a pipe and returns
// what it printed. A goroutine drains the pipe so large outputs cannot
// deadlock.
func captureStdoutOf(fn func()) string {
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		fn()
		return ""
	}
	os.Stdout = w
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	func() {
		defer func() {
			os.Stdout = old
			_ = w.Close()
		}()
		fn()
	}()
	return <-done
}
