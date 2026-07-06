package main

import (
	"os"
	"path/filepath"
	"testing"
)

// newFixture writes files to a temp dir and git-inits+commits them, returning
// the dir. This is the fixture's "initial state" (HEAD) that api/files oracles
// diff against. Hermetic: no network, local git only.
func newFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "oracle-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	if err := materializeFixture(dir, files); err != nil {
		t.Fatal(err)
	}
	if err := gitInitCommit(dir); err != nil {
		t.Fatal(err)
	}
	return dir
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustPass(t *testing.T, dir string, c oracleCheck) {
	t.Helper()
	if ok, msg := evalOne(dir, c); !ok {
		t.Errorf("expected %s to pass, failed: %s", c.Kind, msg)
	}
}

func mustFail(t *testing.T, dir string, c oracleCheck) {
	t.Helper()
	if ok, _ := evalOne(dir, c); ok {
		t.Errorf("expected %s to fail, but it passed", c.Kind)
	}
}

func TestOracleDeclaredAbsent(t *testing.T) {
	dir := newFixture(t, map[string]string{
		"go.mod": gomod,
		"a.go":   "package fixture\n\ntype Widget struct{}\n\nfunc (w *Widget) Render() string { return \"\" }\n\nfunc Free() int { return 1 }\n",
		"b.go":   "package fixture\n\nconst Version = \"1\"\n",
	})
	mustPass(t, dir, declaredIn("Free", "a.go"))
	mustPass(t, dir, declaredIn("Widget", "a.go"))
	mustPass(t, dir, declaredIn("Widget:Render", "a.go"))
	mustPass(t, dir, declaredIn("Version", "b.go"))
	mustPass(t, dir, absentFrom("Free", "b.go"))
	mustPass(t, dir, absentFrom("Widget:Render", "b.go"))
	// Wrong file / wrong receiver / missing symbol → the negations.
	mustFail(t, dir, declaredIn("Free", "b.go"))
	mustFail(t, dir, declaredIn("Widget:Missing", "a.go"))
	mustFail(t, dir, absentFrom("Free", "a.go"))
}

func TestOracleAST(t *testing.T) {
	dir := newFixture(t, map[string]string{
		"go.mod": gomod,
		"a.go":   "package fixture\n\nimport \"errors\"\n\nvar ErrX = errors.New(\"x\")\n",
	})
	mustPass(t, dir, astMatches(`errors.New($_)`))
	mustPass(t, dir, astAbsent(`fmt.Errorf($_)`))
	mustFail(t, dir, astAbsent(`errors.New($_)`))
	mustFail(t, dir, astMatches(`fmt.Errorf($_)`))
}

func TestOracleAPIUnchangedAndAdded(t *testing.T) {
	files := map[string]string{
		"go.mod": gomod,
		"a.go":   "package fixture\n\nfunc Existing() {}\n",
	}
	dir := newFixture(t, files)

	// No working-tree changes → API unchanged, nothing added.
	mustPass(t, dir, apiUnchanged())
	mustFail(t, dir, apiAdded("Added"))

	// Add a new exported function to the working tree.
	writeFile(t, dir, "a.go", "package fixture\n\nfunc Existing() {}\n\nfunc Added() int { return 0 }\n")
	mustPass(t, dir, apiAdded("Added"))
	mustFail(t, dir, apiUnchanged())
}

func TestOracleUsesResolve(t *testing.T) {
	dir := newFixture(t, map[string]string{
		"go.mod":   gomod,
		"lib.go":   "package fixture\n\nfunc Helper() int { return 1 }\n",
		"user.go":  "package fixture\n\nfunc Use() int { return Helper() }\n",
		"other.go": "package fixture\n\nfunc Idle() int { return 2 }\n",
	})
	mustPass(t, dir, usesResolve("Helper", "user.go"))
	mustFail(t, dir, usesResolve("Helper", "other.go"))
}

func TestOracleFilesTouched(t *testing.T) {
	dir := newFixture(t, map[string]string{
		"go.mod": gomod,
		"a.go":   "package fixture\n\nfunc A() {}\n",
		"b.go":   "package fixture\n\nfunc B() {}\n",
	})
	// No changes yet → any allow-set passes (empty change set ⊆ anything).
	mustPass(t, dir, filesTouched("a.go"))

	writeFile(t, dir, "a.go", "package fixture\n\nfunc A() int { return 0 }\n")
	mustPass(t, dir, filesTouched("a.go")) // change is within the allowed set
	mustFail(t, dir, filesTouched("b.go")) // a.go changed but not allowed
}

func TestEvalOracleAggregates(t *testing.T) {
	dir := newFixture(t, map[string]string{
		"go.mod": gomod,
		"a.go":   "package fixture\n\nfunc Keep() {}\n",
	})
	ok, fails := evalOracle(dir, []oracleCheck{declaredIn("Keep", "a.go"), apiUnchanged()})
	if !ok || len(fails) != 0 {
		t.Errorf("expected all-pass, got ok=%v fails=%v", ok, fails)
	}
	ok, fails = evalOracle(dir, []oracleCheck{declaredIn("Missing", "a.go"), declaredIn("Keep", "a.go")})
	if ok || len(fails) != 1 {
		t.Errorf("expected one failure, got ok=%v fails=%v", ok, fails)
	}
	// Empty check list is a vacuous pass.
	if ok, _ := evalOracle(dir, nil); !ok {
		t.Error("empty oracle should pass")
	}
}

// TestOracleRejectsNoOp is the decoy guard: for every corpus task that declares
// an intent-oracle, a lazy agent that changed nothing (fixture left at its
// committed HEAD state) MUST fail the oracle. This proves the asserts genuinely
// require the transform rather than passing vacuously — and it costs no tokens
// because it never invokes the agent.
func TestOracleRejectsNoOp(t *testing.T) {
	for _, task := range agentTasks() {
		if len(task.Assert) == 0 {
			continue
		}
		t.Run(task.ID, func(t *testing.T) {
			dir := newFixture(t, task.Fixture) // materialized + committed, but NOT transformed
			ok, _ := evalOracle(dir, task.Assert)
			if ok {
				t.Errorf("task %q oracle passed on an unchanged fixture — asserts do not require the transform", task.ID)
			}
		})
	}
}
