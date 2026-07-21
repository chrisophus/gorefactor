package main

import (
	"os"
	"testing"
)

func TestTxnJSONEnvelope(t *testing.T) {
	t.Chdir(t.TempDir())
	path := writeTempGo(t, ".", "f.go", "package x\n\nfunc Drop() {}\n\nfunc Keep() {}\n")
	if err := os.WriteFile("plan.txn", []byte("delete "+path+" Drop\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		if err := txnCommand([]string{"plan.txn", "--json"}); err != nil {
			t.Errorf("txn --json: %v", err)
		}
	})
	var res txnResult
	decodeEnvelope(t, out, &res)
	if res.Operation != "txn" || len(res.Ops) != 1 {
		t.Fatalf("unexpected txn result: %+v", res)
	}
	if res.UndoToken == "" || len(res.FilesChanged) != 1 {
		t.Fatalf("success must carry undoToken and filesChanged: %+v", res)
	}

	// Second run fails (Drop is gone) and must produce an ok=false envelope.
	var terr error
	out = captureStdout(t, func() { terr = txnCommand([]string{"plan.txn", "--json"}) })
	if terr == nil {
		t.Fatal("re-running the txn should fail")
	}
	if msg := decodeErrorEnvelope(t, out, nil); msg == "" {
		t.Fatal("error envelope must carry a message")
	}
}
