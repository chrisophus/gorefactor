package main

import (
	"fmt"
	goparser "go/parser"
	"go/token"
	"os"
	"strings"

	"github.com/chrisophus/gorefactor/orchestrator"
)

// txnFail renders a failed transaction for --json consumers and returns the
// error (with its semantic exit code) unchanged. Everything is rolled back
// before this is called, so no files are reported changed.
func txnFail(jsonOut bool, ops []txnOpResult, err error) error {
	if jsonOut {
		emitJSON(txnResult{Success: false, Operation: "txn", Ops: ops, Error: err.Error()})
	}
	return err
}

func txnCommandList() []string {
	return txnSafeCommands()
}

func txnCommand(args []string) error {
	pos, flags := parseFlags(args, txnFlags)
	jsonOut := flags["--json"] != ""
	gate := flags["--gate"] != ""

	script, err := readTxnScript(pos)
	if err != nil {
		return err
	}
	lines, err := parseTxnScript(script)
	if err != nil {
		return txnFail(jsonOut, nil, err)
	}
	if len(lines) == 0 {
		return usageErrorf("txn: no commands in script")
	}

	// The orchestrator journal batch captures every file the sub-commands
	// touch (via RecordOperation) and commits them as one undo unit.
	batch, err := orchestrator.BeginBatch()
	if err != nil {
		return usageErrorf("txn cannot be nested")
	}
	defer orchestrator.EndBatch()

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
			if !isTxnAllowed(ln.argv[0]) {
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
			batch.Rollback()
			return txnFail(jsonOut, ops,
				&cliError{Code: exitCodeFor(runErr), Msg: fmt.Sprintf("txn: line %d (%s) failed: %v — all changes rolled back", ln.line, ln.argv[0], runErr)})
		}
		op.Success = true
		ops = append(ops, op)
	}

	touched := batch.Touched()
	for _, f := range touched {
		if !strings.HasSuffix(f, ".go") {
			continue
		}
		if _, err := os.Stat(f); err != nil {
			continue
		}
		if _, perr := goparser.ParseFile(token.NewFileSet(), f, nil, 0); perr != nil {
			batch.Rollback()
			return txnFail(jsonOut, ops,
				parseErrorf("txn: parse gate failed on %s: %v — all changes rolled back", f, perr))
		}
	}
	if gate {
		if _, err := goGate(".", "build", "./..."); err != nil {
			batch.Rollback()
			return txnFail(jsonOut, ops,
				gateErrorf("txn: build gate failed — all changes rolled back\n%v", err))
		}
	}

	// Commit: one journal entry for the whole batch.
	undoToken := ""
	if !batch.Empty() {
		detail := fmt.Sprintf("txn: %d operation(s), %d file(s)", len(ops), len(touched))
		entry, jerr := batch.Commit("txn", detail)
		if jerr != nil {
			fmt.Fprintf(os.Stderr, "warning: journal write failed: %v\n", jerr)
		} else if entry != nil {
			undoToken = entry.ID
		}
	}

	if jsonOut {
		emitJSON(txnResult{
			Success:      true,
			Operation:    "txn",
			Ops:          ops,
			FilesChanged: touched,
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
	if len(touched) > 0 {
		fmt.Printf("files: %s\n", strings.Join(touched, ", "))
	}
	return nil
}
