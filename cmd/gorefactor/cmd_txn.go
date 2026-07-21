package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

var txnFlags = map[string]bool{"--json": false, "--gate": false}

func init() {
	registerCommand(Command{
		Name:        "txn",
		Mutates:     true,
		MCPTool:     true,
		Description: "Apply a batch of mutation commands transactionally (all-or-nothing, single undo unit)",
		Usage:       "txn [file|-] [--json] [--gate]",
		MinArgs:     0,
		MaxArgs:     1,
		Flags:       txnFlags,
		Run:         txnCommand,
	})
}

// txnSafeCommands are the mutation commands routed through the shared mutation
// runner — the only ones whose effects the transaction can capture and roll
// back. The set is derived from the per-command I/O metadata (TxnSafe) rather
// than hand-maintained, so a command joins txn by setting TxnSafe at
// registration. Read-only commands are excluded on purpose: a txn script is a
// write batch.
func txnSafeCommands() []string {
	return commandsWhere(func(c Command) bool { return c.TxnSafe })
}

// isTxnAllowed reports whether a command may appear as a line in a txn script.
func isTxnAllowed(name string) bool {
	cmd, ok := getCommands()[name]
	return ok && cmd.TxnSafe
}

// The transaction batch (accumulate pre-mutation state, commit as one journal
// entry, roll back as one unit) is owned by the orchestrator journal — see
// orchestrator.Batch and BeginBatch. txn just drives it.

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
	Operation    string        `json:"operation"`
	Ops          []txnOpResult `json:"ops"`
	FilesChanged []string      `json:"filesChanged,omitempty"`
	UndoToken    string        `json:"undoToken,omitempty"`
}

// Parse gate over every touched .go file.

// deleted by a later op

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
