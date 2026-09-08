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

// baseFiles is the first committed state of the fixture module. The change
// under test edits one method of Store, which is enough to exercise every
// role: the method has a caller, a test, a named type in its signature, and a
// sibling implementation of the interface its receiver satisfies.
//
// The fixture commits twice, and the two commits touch different lines of
// Insert. That is what makes the history role's content an assertion: the
// span the change was made against was written by the first commit, so a
// history walk of the wrong span reports the second commit's subject instead.
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

// Inserter reaches Insert without calling it, which is what the caller
// role's label has to distinguish.
var Inserter = (*Store).Insert
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

// storeCommit and guardCommit are the two commit subjects. The lines the
// working change is made against were written by the first, so the history
// role reaching guardCommit instead means it walked the wrong span.
const (
	storeCommit = "add the store: Insert counts the records it is given"
	guardCommit = "reject a record with no identity: an empty ID overwrote the last row"
)

// guardedStore is the second commit: it adds the empty-identity guard above
// the lines the working change is made against, so those lines still belong
// to the first commit.
const guardedStore = `package fix

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
`

// changedStore rewrites Insert so the diff lands inside one method.
//
// It both adds a guard and rewrites the counter line. The rewrite is what
// gives the history role something to trace: a hunk that only inserts has no
// base-side span, and lines that did not exist before have no prior history.
// The counter line was written by the first commit, so tracing the span this
// change displaces must reach that commit and not the second one.
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
	s.n += 1
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

	callers := byRole[RoleCaller]
	if got := callers[0]; got.File != "use.go" || got.Symbol != "Store.Insert" || got.Details["kind"] != "call-site" {
		t.Errorf("caller = %s in %s labelled %q, want Store.Insert in use.go as a call-site",
			got.Symbol, got.File, got.Details["kind"])
	}
	// A method expression reaches the symbol without calling it. The role
	// still carries it, since a reviewer wants to see it, but calling it a
	// call site is the kind of wrong label that costs the role its
	// credibility.
	labelled := ""
	for _, e := range callers {
		if strings.Contains(e.Content, "(*Store).Insert") {
			labelled = e.Details["kind"]
		}
	}
	if labelled != "reference-site" {
		t.Errorf("the method expression that reaches Store.Insert is labelled %q, want reference-site", labelled)
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
	// The span the change was made against was written by the first commit,
	// and the second commit's guard sits above it. Any other span of
	// store.go reports the second commit instead, so this is the assertion
	// that the history role walked the lines the change displaced.
	//
	// Selected by kind rather than by position: removed-line history is
	// ranked above surviving-line history on purpose, so it sorts first.
	var hist Expansion
	for _, e := range byRole[RoleHistory] {
		if e.Details["kind"] == "line-history" {
			hist = e
			break
		}
	}
	if hist.Content == "" {
		t.Fatalf("no surviving-line history expansion: %v", byRole[RoleHistory])
	}
	if !strings.Contains(hist.Content, storeCommit) {
		t.Errorf("history of %s:%d-%d does not reach %q:\n%s",
			hist.File, hist.StartLine, hist.EndLine, storeCommit, hist.Content)
	}
	if strings.Contains(hist.Content, guardCommit) {
		t.Errorf("history of %s:%d-%d reached the second commit, so it walked the wrong span:\n%s",
			hist.File, hist.StartLine, hist.EndLine, hist.Content)
	}
	if hist.Symbol != "Store.Insert" {
		t.Errorf("history of a span inside one changed declaration is labelled %q, want Store.Insert", hist.Symbol)
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
	// The removed-line role is the one under test. Surviving-line history of
	// the same file can reach the same commit now that it walks base, so
	// matching on content alone would pass without the role working.
	var found bool
	for _, x := range env.Expansions {
		if x.Role != RoleHistory || x.Details["kind"] != "removed-line-history" {
			continue
		}
		if strings.Contains(x.Content, "panicked in production") {
			found = true
		}
	}
	if !found {
		t.Fatal("the commit that added the deleted guard did not reach the envelope, " +
			"so nothing distinguishes this change from an ordinary simplification")
	}
	// Removed history outranks the surviving lines' history within the role.
	// The consumer spends a role from the top and drops its tail, and why a
	// line was removed is the sharper question.
	firstRemoved, firstSurviving := -1, -1
	for i, x := range env.Expansions {
		if x.Role != RoleHistory {
			continue
		}
		switch x.Details["kind"] {
		case "removed-line-history":
			if firstRemoved < 0 {
				firstRemoved = i
			}
		case "line-history":
			if firstSurviving < 0 {
				firstSurviving = i
			}
		}
	}
	if firstSurviving >= 0 && firstRemoved > firstSurviving {
		t.Errorf("the removed lines' history is ranked at %d, behind the surviving lines' history at %d, "+
			"so a binding budget drops the deletion first", firstRemoved, firstSurviving)
	}
}

// TestBuildTracesTheHistoryOfADeletedFile is the change that resolves to no
// declaration at all. Removing a file outright leaves nothing to type-check,
// so every role that reads declarations is empty and history is the only
// thing left that can say why the code existed. The deleted-lines test above
// cannot reach this: its deletion sits inside a surviving declaration.
func TestBuildTracesTheHistoryOfADeletedFile(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	t.Setenv("GOWORK", "off")
	dir := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	const subject = "add the drain: the retry path needed a copy it could keep"
	writeFile(t, dir, "go.mod", "module example.com/d\n\ngo 1.26\n")
	writeFile(t, dir, "d.go", "package d\n\n// Drain copies the items.\nfunc Drain(items []string) []string {\n\treturn append([]string(nil), items...)\n}\n")
	gitRun(t, dir, "init", "-q")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "-c", "user.email=fixture@example.com", "-c", "user.name=Fixture",
		"-c", "commit.gpgsign=false", "commit", "-q", "-m", subject)
	gitRun(t, dir, "rm", "-q", "d.go")

	env, err := Build(Options{Root: dir, BaseRef: "HEAD", Version: "v0.0.0-test"})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, x := range env.Expansions {
		if x.Role == RoleHistory && strings.Contains(x.Content, subject) {
			return
		}
	}
	t.Fatalf("removing the file left no history in the envelope, so nothing says why the code existed; "+
		"expansions = %d, notes = %v", len(env.Expansions), env.Notes)
}

// TestBuildEmitsOneExpansionPerTestFunction pins the key the test role
// de-duplicates on. Keying on the symbol as well as the function shipped one
// test function once per changed symbol it touched, byte for byte, and the
// second copy tells a reviewer nothing the first did not.
func TestBuildEmitsOneExpansionPerTestFunction(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	t.Setenv("GOWORK", "off")
	dir := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	writeFile(t, dir, "go.mod", "module example.com/two\n\ngo 1.26\n")
	writeFile(t, dir, "two.go", "package two\n\n// A is the first half.\nfunc A() int { return 1 }\n\n// B is the second half.\nfunc B() int { return 2 }\n")
	writeFile(t, dir, "two_test.go", "package two\n\nimport \"testing\"\n\nfunc TestBoth(t *testing.T) {\n\tif A()+B() != 3 {\n\t\tt.Fatal(\"sum\")\n\t}\n}\n")
	gitRun(t, dir, "init", "-q")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "-c", "user.email=fixture@example.com", "-c", "user.name=Fixture",
		"-c", "commit.gpgsign=false", "commit", "-q", "-m", "add both halves")
	writeFile(t, dir, "two.go", "package two\n\n// A is the first half.\nfunc A() int { return 10 }\n\n// B is the second half.\nfunc B() int { return 20 }\n")

	env, err := Build(Options{Root: dir, BaseRef: "HEAD", Version: "v0.0.0-test"})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	var tests []Expansion
	for _, x := range env.Expansions {
		if x.Role == RoleTest {
			tests = append(tests, x)
		}
	}
	if len(tests) != 1 {
		t.Fatalf("TestBoth reaches two changed symbols and produced %d test expansions; want one naming both: %v",
			len(tests), symbolsOf(tests))
	}
	if tests[0].Symbol != "TestBoth" || tests[0].Details["covers"] != "A, B" {
		t.Errorf("test expansion = %s covering %q, want TestBoth covering \"A, B\"",
			tests[0].Symbol, tests[0].Details["covers"])
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

// fixtureRepo builds a module in two commits and leaves one method edited in
// the working tree. The two commits touch different lines of Insert so the
// history role's content says which span it walked.
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
	commit := func(msg string) {
		gitRun(t, dir, "add", "-A")
		gitRun(t, dir, "-c", "user.email=fixture@example.com", "-c", "user.name=Fixture",
			"-c", "commit.gpgsign=false", "commit", "-q", "-m", msg)
	}
	gitRun(t, dir, "init", "-q")
	commit(storeCommit)
	writeFile(t, dir, "store.go", guardedStore)
	commit(guardCommit)
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
