package main

import (
	"fmt"
	goparser "go/parser"
	"go/token"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/chrisophus/gorefactor/orchestrator"
)

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

// Commit: one journal entry for the whole batch.

func txnCommandList() []string {
	var names []string
	for n := range txnAllowedCommands {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func txnCommand(args []string) error {
	pos, flags := parseFlags(args, txnFlags)
	jsonOut := flags["--json"] != ""
	gate := flags["--gate"] != ""

	if activeTxn != nil {
		return usageErrorf("txn cannot be nested")
	}

	script, err := readTxnScript(pos)
	if err != nil {
		return err
	}
	lines, err := parseTxnScript(script)
	if err != nil {
		return txnFail(jsonOut, nil, nil, err)
	}
	if len(lines) == 0 {
		return usageErrorf("txn: no commands in script")
	}

	collector := newTxnCollector()
	activeTxn = collector
	defer func() { activeTxn = nil }()

	var ops []txnOpResult
	for _, ln := range lines {
		op := txnOpResult{Line: ln.line, Command: ln.argv[0]}
		if len(ln.argv) > 1 {
			op.Args = strings.Join(ln.argv[1:], " ")
		}
		runErr := func() error {
			cmd, ok := getCommands()[ln.argv[0]]
			if !ok {
				return usageErrorf("unknown command %q", ln.argv[0])
			}
			if !txnAllowedCommands[ln.argv[0]] {
				return usageErrorf("command %q is not allowed inside txn (only mutation commands: %s)",
					ln.argv[0], strings.Join(txnCommandList(), ", "))
			}
			if err := checkCommandArgs(cmd, ln.argv[1:]); err != nil {
				return err
			}
			var err error
			op.Output = captureStdoutOf(func() { err = cmd.Run(ln.argv[1:]) })
			return err
		}()
		if runErr != nil {
			op.Success = false
			op.Error = runErr.Error()
			ops = append(ops, op)
			collector.restore()
			return txnFail(jsonOut, ops, collector,
				&cliError{code: exitCodeFor(runErr), msg: fmt.Sprintf("txn: line %d (%s) failed: %v — all changes rolled back", ln.line, ln.argv[0], runErr)})
		}
		op.Success = true
		ops = append(ops, op)
	}

	for _, f := range collector.touched() {
		if !strings.HasSuffix(f, ".go") {
			continue
		}
		if _, err := os.Stat(f); err != nil {
			continue
		}
		if _, perr := goparser.ParseFile(token.NewFileSet(), f, nil, 0); perr != nil {
			collector.restore()
			return txnFail(jsonOut, ops, collector,
				parseErrorf("txn: parse gate failed on %s: %v — all changes rolled back", f, perr))
		}
	}
	if gate {
		if out, err := exec.Command("go", "build", "./...").CombinedOutput(); err != nil {
			collector.restore()
			return txnFail(jsonOut, ops, collector,
				gateErrorf("txn: build gate failed — all changes rolled back\n%s", strings.TrimSpace(string(out))))
		}
	}

	undoToken := ""
	if len(collector.seen) > 0 {
		var created []string
		for p := range collector.created {
			created = append(created, p)
		}
		sort.Strings(created)
		detail := fmt.Sprintf("txn: %d operation(s), %d file(s)", len(ops), len(collector.seen))
		entry, jerr := orchestrator.RecordOperation("txn", detail, collector.before, created)
		if jerr != nil {
			fmt.Fprintf(os.Stderr, "warning: journal write failed: %v\n", jerr)
		} else {
			undoToken = entry.ID
		}
	}

	if jsonOut {
		emitJSON(txnResult{
			Success:      true,
			Operation:    "txn",
			Ops:          ops,
			FilesChanged: collector.touched(),
			UndoToken:    undoToken,
		})
		return nil
	}
	fmt.Printf("txn: %d operation(s) applied as one unit\n", len(ops))
	for _, op := range ops {
		summary := strings.TrimSpace(op.Output)
		if i := strings.IndexByte(summary, '\n'); i >= 0 {
			summary = summary[:i]
		}
		fmt.Printf("  line %2d  %-15s %s\n", op.Line, op.Command, summary)
	}
	if len(collector.touched()) > 0 {
		fmt.Printf("files: %s\n", strings.Join(collector.touched(), ", "))
	}
	return nil
}
