package changectx

// SchemaVersion is the version of the wire format this package writes. The
// consumer refuses a version it does not know rather than reading fields that
// may have moved.
const SchemaVersion = 1

// providerName and providerLanguage identify this provider in every envelope.
const (
	providerName     = "gorefactor"
	providerLanguage = "go"
)

// Role is what an expansion is. The vocabulary belongs to the consumer, which
// ranks by role without parsing the code an expansion carries. Inventing a
// role here would rank it last on the far side, so the set below is closed.
type Role string

const (
	// RoleEnclosing is the whole declaration a changed hunk sits inside.
	RoleEnclosing Role = "enclosing"
	// RoleCaller is a call site of a changed exported symbol.
	RoleCaller Role = "caller"
	// RoleType is the definition of a type named in a changed signature.
	RoleType Role = "type"
	// RoleSibling is another implementation of an interface a changed type
	// implements.
	RoleSibling Role = "sibling"
	// RoleTest is a test that covers a changed symbol.
	RoleTest Role = "test"
	// RoleHistory is prior history of the changed lines.
	RoleHistory Role = "history"
)

// roleRank orders expansions in the emitted JSON. It mirrors the consumer's
// own ranking so the output reads in the order it will be spent, and it has no
// effect on what the consumer keeps.
var roleRank = map[Role]int{
	RoleEnclosing: 0,
	RoleCaller:    1,
	RoleType:      2,
	RoleSibling:   3,
	RoleTest:      4,
	RoleHistory:   5,
}

// Class is what a changed file is.
type Class string

const (
	ClassSource    Class = "source"
	ClassGenerated Class = "generated"
	ClassVendored  Class = "vendored"
	ClassTest      Class = "test"
	ClassMigration Class = "migration"
	ClassLockfile  Class = "lockfile"
	ClassOther     Class = "other"
)

// Provider identifies what produced an envelope. The version travels with the
// data because the Go half of the review knowledge lives in this repository
// and is spent in another one, so a frozen fixture can say who wrote it.
type Provider struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	Language string `json:"language,omitempty"`
}

// File is one changed file in the manifest.
type File struct {
	Path      string   `json:"path"`
	Class     Class    `json:"class"`
	Generated bool     `json:"generated,omitempty"`
	Symbols   []string `json:"symbols,omitempty"`
}

// Expansion is one piece of context beyond the diff. Symbol and Scope are
// opaque strings on the far side. Anything that only makes sense for Go goes
// in Details, which the consumer renders generically.
type Expansion struct {
	Role      Role              `json:"role"`
	Priority  int               `json:"priority,omitempty"`
	Symbol    string            `json:"symbol,omitempty"`
	Scope     string            `json:"scope,omitempty"`
	File      string            `json:"file,omitempty"`
	StartLine int               `json:"startLine,omitempty"`
	EndLine   int               `json:"endLine,omitempty"`
	Content   string            `json:"content"`
	Details   map[string]string `json:"details,omitempty"`
}

// Envelope is this provider's answer for one change.
type Envelope struct {
	SchemaVersion  int         `json:"schemaVersion"`
	Provider       Provider    `json:"provider"`
	BaseSHA        string      `json:"baseSHA,omitempty"`
	Files          []File      `json:"files,omitempty"`
	Expansions     []Expansion `json:"expansions,omitempty"`
	PromptFragment string      `json:"promptFragment,omitempty"`
	Notes          []string    `json:"notes,omitempty"`
}
