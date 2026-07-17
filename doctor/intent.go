package doctor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Intent is a session-scoped declaration that an API change is deliberate.
// Records live in .gorefactor/intents.json (gitignored, like the journal), are
// written by the CLI, orchestrator plans, or the agent spec, and are scoped to
// a package or symbol prefix precisely so a campaign cannot blanket-declare
// its way past the apidiff gate.
type Intent struct {
	Type    string    `json:"type"`  // currently always "api-change"
	Scope   string    `json:"scope"` // package dir or symbol prefix, e.g. "analyzer" or "analyzer.ComputeAPIDiff"
	Reason  string    `json:"reason"`
	Created time.Time `json:"created"`
}

// Matches reports whether symbol falls inside the intent's declared scope.
// A scope matches the symbol itself or any name nested under it ('.' for
// symbols, '/' for package dirs), never a mere string prefix — declaring
// "analyzer" must not cover "analyzer2".
func (in Intent) Matches(symbol string) bool {
	if in.Scope == symbol {
		return true
	}
	return strings.HasPrefix(symbol, in.Scope+".") || strings.HasPrefix(symbol, in.Scope+"/")
}

// IntentAPIChange is the intent type the apidiff gate consumes.
const IntentAPIChange = "api-change"

const intentsFileName = "intents.json"

// LoadIntents reads the intent records for root. A missing file is no intents.
func LoadIntents(root string) ([]Intent, error) {
	data, err := os.ReadFile(intentsPath(root))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read intents: %w", err)
	}
	var intents []Intent
	if err := json.Unmarshal(data, &intents); err != nil {
		return nil, fmt.Errorf("parse %s: %w", intentsPath(root), err)
	}
	return intents, nil
}

// AddIntent appends one intent record.
func AddIntent(root string, in Intent) error {
	if in.Scope == "" {
		return fmt.Errorf("intent requires a package or symbol scope")
	}
	if in.Created.IsZero() {
		in.Created = time.Now().UTC()
	}
	intents, err := LoadIntents(root)
	if err != nil {
		return fmt.Errorf("load intents: %w", err)
	}
	intents = append(intents, in)
	data, err := json.MarshalIndent(intents, "", "  ")
	if err != nil {
		return fmt.Errorf("encode intents: %w", err)
	}
	path := intentsPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create .gorefactor dir: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write intents: %w", err)
	}
	return nil
}

// ClearIntents removes all intent records — called when the session or
// campaign that declared them ends.
func ClearIntents(root string) error {
	err := os.Remove(intentsPath(root))
	if err == nil || os.IsNotExist(err) {
		return nil
	}
	return fmt.Errorf("clear intents: %w", err)
}

func intentsPath(root string) string {
	return filepath.Join(root, ".gorefactor", intentsFileName)
}

// matchIntent returns the first api-change intent covering symbol, if any.
func matchIntent(intents []Intent, symbol string) (Intent, bool) {
	for _, in := range intents {
		if in.Type == IntentAPIChange && in.Matches(symbol) {
			return in, true
		}
	}
	return Intent{}, false
}
