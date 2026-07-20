package main

import (
	"reflect"
	"testing"
)

// The tests in this file are the doc-drift / registry guards for the P2 "one
// I/O contract" metadata (registry.go Command.ReadOnly/Mutates/Idempotent/
// MCPTool/TxnSafe). The MCP and txn allowlists are derived from that metadata
// (mcpReadOnlyTools, mcpWriteTools, txnSafeCommands), so these tests make it
// impossible for a new command to silently skip classification or slip into a
// safety-sensitive surface without a deliberate, reviewed change here.

// TestCommandMetadata_ReadOnlyXorMutates forces every registered command to
// declare exactly one of ReadOnly / Mutates. This is the core "cannot skip
// metadata" gate: a new command that forgets to classify itself fails here.
func TestCommandMetadata_ReadOnlyXorMutates(t *testing.T) {
	for name, cmd := range getCommands() {
		if cmd.ReadOnly == cmd.Mutates {
			t.Errorf("command %q must set exactly one of ReadOnly / Mutates (got ReadOnly=%v, Mutates=%v)",
				name, cmd.ReadOnly, cmd.Mutates)
		}
	}
}

// TestCommandMetadata_WriteOnlyFlagsImplyMutates keeps the write-only facets
// (Idempotent, TxnSafe) meaningful: they only make sense for a command that
// actually changes files.
func TestCommandMetadata_WriteOnlyFlagsImplyMutates(t *testing.T) {
	for name, cmd := range getCommands() {
		if cmd.Idempotent && !cmd.Mutates {
			t.Errorf("command %q sets Idempotent but not Mutates", name)
		}
		if cmd.TxnSafe && !cmd.Mutates {
			t.Errorf("command %q sets TxnSafe but not Mutates", name)
		}
	}
}

// txnExempt lists MCP write tools that are deliberately NOT txn-safe, with the
// reason. Every mutating MCP tool must be either TxnSafe or listed here — so a
// new mutating tool cannot quietly skip the txn-allowlist decision.
var txnExempt = map[string]string{
	"txn":  "a transaction cannot be nested inside another transaction",
	"undo": "restores from the journal; it is not a mutation-runner operation",
}

// TestCommandMetadata_MCPWriteToolsAreTxnSafeOrExempt guarantees that every
// mutation command exposed over MCP has a considered txn classification: it is
// routable through a txn (TxnSafe) or explicitly exempt with a documented
// reason. This is the concrete "every Mutates command is in the txn allowlist
// OR explicitly marked" invariant, scoped to the MCP write surface (the set of
// real source mutators most likely to grow).
func TestCommandMetadata_MCPWriteToolsAreTxnSafeOrExempt(t *testing.T) {
	for _, name := range mcpWriteTools() {
		cmd := getCommands()[name]
		if cmd.TxnSafe {
			continue
		}
		if _, ok := txnExempt[name]; !ok {
			t.Errorf("MCP write tool %q is neither TxnSafe nor in txnExempt; "+
				"add TxnSafe:true at registration or record a reason in txnExempt", name)
		}
	}
	// And no stale exemptions: an exempt name must still be a mutating tool.
	for name := range txnExempt {
		cmd, ok := getCommands()[name]
		if !ok || !cmd.Mutates {
			t.Errorf("txnExempt lists %q but it is not a registered mutation command", name)
		}
	}
}

// TestCommandMetadata_AllowlistsMatchGolden pins the exact derived surfaces so
// any accidental addition/removal (e.g. tagging a command MCPTool by mistake)
// fails loudly, mirroring lint_registry_test.go's golden pinning. Update these
// lists deliberately when the tool/txn surface intentionally changes.
func TestCommandMetadata_AllowlistsMatchGolden(t *testing.T) {
	wantReadOnly := []string{
		"api-diff", "blast-radius", "callgraph", "context", "find-callers",
		"find-implementations", "find-package-deps", "find-uses", "inspect",
		"lint", "list-functions", "parse", "recommend", "review", "search-ast",
		"skeleton", "suggest-plan", "test-affected",
	}
	wantWrite := []string{
		"add-field", "change-receiver", "change-signature", "create", "delete",
		"extract", "format", "inline", "insert", "move", "rename", "replace",
		"replace-body", "replace-text", "set-doc", "txn", "undo",
	}
	wantTxn := []string{
		"add-field", "change-receiver", "change-signature", "create", "delete",
		"extract", "format", "inline", "insert", "move", "rename", "replace",
		"replace-body", "replace-text", "set-doc", "split",
	}
	wantIdempotent := []string{"format"}

	assertNameSet(t, "mcp read-only tools", mcpReadOnlyTools(), wantReadOnly)
	assertNameSet(t, "mcp write tools", mcpWriteTools(), wantWrite)
	assertNameSet(t, "txn-safe commands", txnSafeCommands(), wantTxn)
	assertNameSet(t, "idempotent commands",
		commandsWhere(func(c Command) bool { return c.Idempotent }), wantIdempotent)
}

func assertNameSet(t *testing.T, label string, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s drifted from the golden set.\n got: %v\nwant: %v\n"+
			"If this change is intentional, update the golden list in registry_metadata_test.go.",
			label, got, want)
	}
}
