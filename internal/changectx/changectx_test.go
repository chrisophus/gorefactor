package changectx

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chrisophus/gorefactor/analyzer"
)

// baseFiles is the committed state of the fixture module. The change under
// test edits one method of Store, which is enough to exercise every role: the
// method has a caller, a test, a named type in its signature, and a sibling
// implementation of the interface its receiver satisfies.
var baseFiles = map[string]string{
	"go.mod": "module example.com/fix\n\ngo 1.21\n",
	"types.go": `package fix

import "errors"

// Record is one stored row.
type Record struct {
	ID string
}

// ErrEmpty reports a record with no identity.
var ErrEmpty = errors.New("empty record")

// Writer stores records.
type Writer interface {
	Insert(r Record) error
}
`,
	"store.go": `package fix

// Store counts the records it was given.
type Store struct {
	n int
}

// Insert stores a record.
func (s *Store) Insert(r Record) error {
	if r.ID == "" {
		return ErrEmpty
	}
	s.n++
	return nil
}
`,
	"mem.go": `package fix

// MemStore keeps records in memory.
type MemStore struct {
	items []Record
}

// Insert stores a record in memory.
func (m *MemStore) Insert(r Record) error {
	m.items = append(m.items, r)
	return nil
}
`,
	"use.go": `package fix

// Save writes a record through a store.
func Save(s *Store, r Record) error {
	return s.Insert(r)
}
`,
	"store_test.go": `package fix

import "testing"

func TestInsert(t *testing.T) {
	var s Store
	if err := s.Insert(Record{ID: "a"}); err != nil {
		t.Fatalf("insert: %v", err)
	}
}
`,
}

// changedStore rewrites Insert so the diff lands inside one method.
const changedStore = `package fix

// Store counts the records it was given.
type Store struct {
	n int
}

// Insert stores a record.
func (s *Store) Insert(r Record) error {
	if r.ID == "" {
		return ErrEmpty
	}
	if len(r.ID) > 64 {
		return ErrEmpty
	}
	s.n++
	return nil
}
`

func TestBuildEnvelopeFrame(t *testing.T) {
	dir := fixtureRepo(t)
	env := buildFixture(t, dir)

	if env.SchemaVersion != SchemaVersion {
		t.Errorf("schemaVersion = %d, want %d", env.SchemaVersion, SchemaVersion)
	}
	if env.Provider.Name != "gorefactor" || env.Provider.Language != "go" || env.Provider.Version != "v0.0.0-test" {
		t.Errorf("provider = %+v", env.Provider)
	}
	if len(env.BaseSHA) < 7 {
		t.Errorf("baseSHA = %q", env.BaseSHA)
	}
	if !strings.Contains(env.PromptFragment, "%w") {
		t.Error("promptFragment does not carry the Go review half")
	}
	if len(env.Files) != 1 || env.Files[0].Path != "store.go" {
		t.Fatalf("files = %+v, want just store.go", env.Files)
	}
	f := env.Files[0]
	if f.Class != ClassSource || f.Generated {
		t.Errorf("store.go classified %q generated=%v", f.Class, f.Generated)
	}
	if len(f.Symbols) != 1 || f.Symbols[0] != "Store.Insert" {
		t.Errorf("store.go symbols = %v, want [Store.Insert]", f.Symbols)
	}
	for _, note := range env.Notes {
		t.Logf("note: %s", note)
	}
}

func TestBuildExpansionRoles(t *testing.T) {
	dir := fixtureRepo(t)
	env := buildFixture(t, dir)

	byRole := map[Role][]Expansion{}
	for _, e := range env.Expansions {
		byRole[e.Role] = append(byRole[e.Role], e)
	}
	for _, role := range []Role{RoleEnclosing, RoleCaller, RoleType, RoleSibling, RoleTest, RoleHistory} {
		if len(byRole[role]) == 0 {
			t.Errorf("no %s expansion; roles present: %v", role, rolesOf(env))
		}
	}

	enc := byRole[RoleEnclosing][0]
	if enc.Symbol != "Store.Insert" || enc.File != "store.go" {
		t.Errorf("enclosing = %s in %s, want Store.Insert in store.go", enc.Symbol, enc.File)
	}
	// The whole declaration, from its doc comment to its closing brace.
	if !strings.HasPrefix(enc.Content, "// Insert stores a record.") || !strings.HasSuffix(enc.Content, "}\n") {
		t.Errorf("enclosing content is not the whole declaration:\n%s", enc.Content)
	}
	if !strings.Contains(enc.Content, "len(r.ID) > 64") {
		t.Errorf("enclosing content missing the change:\n%s", enc.Content)
	}
	if enc.Details["kind"] != "method" || enc.Details["receiver"] != "Store" {
		t.Errorf("enclosing details = %v", enc.Details)
	}

	if got := byRole[RoleCaller][0]; got.File != "use.go" || got.Symbol != "Store.Insert" {
		t.Errorf("caller = %s in %s, want Store.Insert in use.go", got.Symbol, got.File)
	}
	if got := byRole[RoleTest][0]; got.Symbol != "TestInsert" || got.Details["covers"] != "Store.Insert" {
		t.Errorf("test = %s covering %q", got.Symbol, got.Details["covers"])
	}
	if !hasSymbol(byRole[RoleType], "Record") {
		t.Errorf("type role does not carry Record: %v", symbolsOf(byRole[RoleType]))
	}
	sib := byRole[RoleSibling][0]
	if sib.Symbol != "MemStore" || sib.Details["interface"] != "Writer" {
		t.Errorf("sibling = %s via %q, want MemStore via Writer", sib.Symbol, sib.Details["interface"])
	}
	if !strings.Contains(byRole[RoleHistory][0].Content, "commit ") {
		t.Errorf("history content is not a git log:\n%s", byRole[RoleHistory][0].Content)
	}
}

// TestBuildRoleOrderAndPriority pins the two fields the consumer ranks by.
func TestBuildRoleOrderAndPriority(t *testing.T) {
	dir := fixtureRepo(t)
	env := buildFixture(t, dir)

	last := -1
	for _, e := range env.Expansions {
		rank, known := roleRank[e.Role]
		if !known {
			t.Fatalf("expansion carries role %q, which the consumer does not rank", e.Role)
		}
		if rank < last {
			t.Fatalf("expansions are not grouped by role rank: %q after rank %d", e.Role, last)
		}
		last = rank
	}
	prev := map[Role]int{}
	for _, e := range env.Expansions {
		if p, seen := prev[e.Role]; seen && e.Priority > p {
			t.Errorf("%s priority %d follows %d; priority must descend within a role", e.Role, e.Priority, p)
		}
		prev[e.Role] = e.Priority
	}
}

// TestBuildIsDeterministic is the property the consumer depends on: an eval
// that cannot reproduce its own input is measuring noise.
func TestBuildIsDeterministic(t *testing.T) {
	dir := fixtureRepo(t)

	first, err := json.Marshal(buildFixture(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	second, err := json.Marshal(buildFixture(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Errorf("two runs of the same revision differ:\n%s\n\n%s", first, second)
	}
	if strings.Contains(string(first), dir) {
		t.Errorf("envelope leaks the absolute path %s", dir)
	}
	if len(first) < 100 {
		t.Errorf("envelope is suspiciously small: %s", first)
	}
}

// TestBuildSurvivesBrokenPackage is the failure mode that matters most: a
// provider that gives up because one package does not compile is useless on
// exactly the changes people most want reviewed.
func TestBuildSurvivesBrokenPackage(t *testing.T) {
	dir := fixtureRepo(t)
	writeFile(t, dir, "broken/broken.go", "package broken\n\nfunc Broken() int {\n\treturn \"not an int\"\n}\n")

	env := buildFixture(t, dir)
	var enclosing []Expansion
	for _, e := range env.Expansions {
		if e.Role == RoleEnclosing {
			enclosing = append(enclosing, e)
		}
	}
	if !hasSymbol(enclosing, "Store.Insert") {
		t.Errorf("the working change was dropped when another package broke: %v", symbolsOf(enclosing))
	}
	reported := false
	for _, n := range env.Notes {
		if strings.Contains(n, "type-check") {
			reported = true
		}
	}
	if !reported {
		t.Errorf("a type-check failure went unreported; notes = %v", env.Notes)
	}
}

func TestSummaryCountsRolesAndClasses(t *testing.T) {
	dir := fixtureRepo(t)
	out := Summary(buildFixture(t, dir))
	for _, want := range []string{"context envelope v1", "files: 1 (source 1)", "symbols: 1", "enclosing 1"} {
		if !strings.Contains(out, want) {
			t.Errorf("summary missing %q:\n%s", want, out)
		}
	}
}

// A deletion is where history earns its place: the lines are gone, so the
// diff carries no trace of why they were there, and a guard removed on
// purpose reads exactly like a simplification.
func TestBuildTracesTheHistoryOfDeletedLines(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	t.Setenv("GOWORK", "off")
	dir := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	commit := func(msg string) {
		gitRun(t, dir, "add", "-A")
		gitRun(t, dir, "-c", "user.email=fixture@example.com", "-c", "user.name=Fixture",
			"-c", "commit.gpgsign=false", "commit", "-q", "-m", msg)
	}
	const plain = `package q

func Drain(items []string) []string {
	var out []string
	for _, it := range items {
		out = append(out, it)
	}
	return out
}
`
	const guarded = `package q

func Drain(items []string) []string {
	var out []string
	for _, it := range items {
		if it == "" {
			continue
		}
		out = append(out, it)
	}
	return out
}
`
	writeFile(t, dir, "go.mod", "module example.com/q\n\ngo 1.26\n")
	writeFile(t, dir, "q.go", plain)
	gitRun(t, dir, "init", "-q")
	commit("add the queue")
	writeFile(t, dir, "q.go", guarded)
	commit("skip empty entries: a nil from the retry path panicked in production")
	writeFile(t, dir, "q.go", plain)

	env, err := Build(Options{Root: dir, BaseRef: "HEAD", Version: "v0.0.0-test"})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	var found bool
	for _, x := range env.Expansions {
		if x.Role != RoleHistory {
			continue
		}
		if strings.Contains(x.Content, "panicked in production") {
			found = true
			if x.Details["kind"] != "removed-line-history" {
				t.Errorf("kind = %q, want removed-line-history", x.Details["kind"])
			}
		}
	}
	if !found {
		t.Fatal("the commit that added the deleted guard did not reach the envelope, " +
			"so nothing distinguishes this change from an ordinary simplification")
	}
}

func rolesOf(env *Envelope) []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range env.Expansions {
		if !seen[string(e.Role)] {
			seen[string(e.Role)] = true
			out = append(out, string(e.Role))
		}
	}
	return out
}

func symbolsOf(exps []Expansion) []string {
	out := make([]string, 0, len(exps))
	for _, e := range exps {
		out = append(out, e.Symbol)
	}
	return out
}

func hasSymbol(exps []Expansion, name string) bool {
	for _, e := range exps {
		if e.Symbol == name {
			return true
		}
	}
	return false
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	// Pinned dates keep the history role's content a function of the fixture
	// rather than of the clock the test ran on.
	cmd.Env = append(analyzer.SanitizedGitEnv(),
		"GIT_AUTHOR_DATE=2026-01-02T03:04:05+00:00",
		"GIT_COMMITTER_DATE=2026-01-02T03:04:05+00:00")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// fixtureRepo builds a committed module and leaves one method edited in the
// working tree.
func fixtureRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	t.Setenv("GOWORK", "off")
	dir := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	for rel, content := range baseFiles {
		writeFile(t, dir, rel, content)
	}
	gitRun(t, dir, "init", "-q")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "-c", "user.email=fixture@example.com", "-c", "user.name=Fixture",
		"-c", "commit.gpgsign=false", "commit", "-q", "-m", "base")
	writeFile(t, dir, "store.go", changedStore)
	return dir
}

func buildFixture(t *testing.T, dir string) *Envelope {
	t.Helper()
	env, err := Build(Options{Root: dir, BaseRef: "HEAD", Version: "v0.0.0-test"})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return env
}
