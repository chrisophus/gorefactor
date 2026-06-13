package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeAffectedFixture builds a tiny module: root imports lib, util is isolated.
func writeAffectedFixture(t *testing.T) {
	t.Helper()
	if err := os.WriteFile("go.mod", []byte("module fixture\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"lib", "util"} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}
	writeTempGo(t, "lib", "lib.go", "package lib\n\nfunc Answer() int { return 42 }\n")
	writeTempGo(t, "lib", "lib_test.go", "package lib\n\nimport \"testing\"\n\nfunc TestAnswer(t *testing.T) {\n\tif Answer() != 42 {\n\t\tt.Fatal(\"wrong\")\n\t}\n}\n")
	writeTempGo(t, "util", "util.go", "package util\n\nfunc Other() int { return 1 }\n")
	writeTempGo(t, ".", "root.go", "package main\n\nimport \"fixture/lib\"\n\nfunc main() { _ = lib.Answer() }\n")
}

func TestAffectedExpandsReverseImports(t *testing.T) {
	t.Chdir(t.TempDir())
	writeAffectedFixture(t)
	initGitFixture(t)

	// change lib -> lib is direct, root is affected via reverse import,
	// util must stay untouched.
	writeTempGo(t, "lib", "lib.go", "package lib\n\nfunc Answer() int { return 43 }\n")

	out := captureStdout(t, func() {
		if err := testAffectedCommand([]string{"--json"}); err != nil {
			t.Errorf("test-affected: %v", err)
		}
	})
	var res testAffectedResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if res.Base != "HEAD" || len(res.ChangedFiles) != 1 || res.ChangedFiles[0] != "lib/lib.go" {
		t.Fatalf("unexpected change set: %+v", res)
	}
	got := map[string]affectedPackage{}
	for _, p := range res.Packages {
		got[p.Path] = p
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 affected packages, got %+v", res.Packages)
	}
	if p := got["./lib"]; !p.Direct || !p.HasTests {
		t.Fatalf("./lib should be direct with tests: %+v", p)
	}
	if p := got["."]; p.Direct || p.HasTests {
		t.Fatalf("root should be indirect without tests: %+v", p)
	}
	if _, ok := got["./util"]; ok {
		t.Fatal("util is unrelated and must not be affected")
	}
}

func TestAffectedNoChanges(t *testing.T) {
	t.Chdir(t.TempDir())
	writeAffectedFixture(t)
	initGitFixture(t)

	out := captureStdout(t, func() {
		if err := testAffectedCommand(nil); err != nil {
			t.Errorf("test-affected: %v", err)
		}
	})
	if !strings.Contains(out, "0 file(s) -> 0 affected package(s)") {
		t.Fatalf("clean tree should affect nothing:\n%s", out)
	}
}

func TestAffectedRunFailingTestExitsGate(t *testing.T) {
	t.Chdir(t.TempDir())
	writeAffectedFixture(t)
	initGitFixture(t)

	// break lib so its test fails
	writeTempGo(t, "lib", "lib.go", "package lib\n\nfunc Answer() int { return 0 }\n")

	var cmdErr error
	out := captureStdout(t, func() {
		cmdErr = testAffectedCommand([]string{"--run"})
	})
	assertExitCode(t, cmdErr, exitGateFailure)
	if !strings.Contains(out, "go test: FAIL") {
		t.Fatalf("expected FAIL verdict:\n%s", out)
	}
}

func TestAffectedRunPassing(t *testing.T) {
	t.Chdir(t.TempDir())
	writeAffectedFixture(t)
	initGitFixture(t)

	writeTempGo(t, "lib", "lib_extra.go", "package lib\n\nfunc Extra() int { return Answer() }\n")

	var cmdErr error
	out := captureStdout(t, func() {
		cmdErr = testAffectedCommand([]string{"--run", "--json"})
	})
	if cmdErr != nil {
		t.Fatalf("test-affected --run: %v", cmdErr)
	}
	var res testAffectedResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if !res.Ran || !res.Passed {
		t.Fatalf("expected passing run: %+v", res)
	}
	_ = filepath.Join // keep import if fixture changes
}
