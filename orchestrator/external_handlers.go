package orchestrator

// ExternalOperationHandler executes a plan operation whose engine lives
// outside this package (e.g. the CLI's type-aware extractor, which needs
// go/packages and lives in cmd/gorefactor). The handler receives the
// operation and its resolved semantic target (nil when the operation had no
// target spec) and returns the changes it made.
//
// This registry exists because plan-level extract_method/inline_method were
// advertised (templates, CLAUDE.md) but not dispatchable: the engines were
// CLI-only, so `orchestrate` failed with "unknown operation type" on the
// tool's own generated templates. Handlers are wired at init time by the
// binary that owns the engine.
type ExternalOperationHandler func(op *RefactoringOperation, target *TargetLocation) ([]*CodeChange, error)

var externalHandlers = map[string]ExternalOperationHandler{}

// RegisterExternalHandler wires an operation type to an external engine.
// Init-time wiring only; not safe for concurrent registration.
func RegisterExternalHandler(opType string, h ExternalOperationHandler) {
	externalHandlers[opType] = h
}

// builtinOperationTypes lists the operation types dispatchOperation handles
// in-package. Kept adjacent to the registry so KnownOperationTypes stays the
// single source for "what can a plan contain"; a dispatch-probe test pins it
// to the real switch.
var builtinOperationTypes = []string{
	"move_method",
	"insert_code",
	"create_file",
	"remove_code_block",
	"replace_code",
	"delete_declaration",
	"rename_declaration",
}

// KnownOperationTypes returns every operation type a plan can currently
// execute: the built-in dispatch set plus any registered external handlers.
func KnownOperationTypes() []string {
	out := append([]string{}, builtinOperationTypes...)

	for t := range externalHandlers {
		out = append(out, t)
	}
	return out
}
