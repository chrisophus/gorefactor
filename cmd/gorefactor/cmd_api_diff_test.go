package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// initGitFixture turns the current directory into a git repo with one commit.
func initGitFixture(t *testing.T) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-q"}, {"config", "user.email", "t@e.com"},
		{"config", "user.name", "t"}, {"config", "commit.gpgsign", "false"},
		{"add", "-A"}, {"commit", "-q", "-m", "init"},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

const apiDiffV1 = `package api

// Keep stays the same.
func Keep(a int) int { return a }

// Gone will be removed.
func Gone() {}

// Sig will change signature.
func Sig(a int) int { return a }

type Cfg struct {
	Name string
	Old  int
}

const Limit = 5
`

const apiDiffV2 = `package api

// Keep stays the same.
func Keep(a int) int { return a }

// Sig will change signature.
func Sig(a int, b string) int { return a }

// Fresh is brand new.
func Fresh() {}

type Cfg struct {
	Name  string
	Extra bool
}

const Limit = 5

func unexported() {} // never part of the API surface
`

func TestAPIDiffDetectsChanges(t *testing.T) {
	t.Chdir(t.TempDir())
	writeTempGo(t, ".", "api.go", apiDiffV1)
	initGitFixture(t)
	writeTempGo(t, ".", "api.go", apiDiffV2)

	out := captureStdout(t, func() {
		if err := apiDiffCommand([]string{"--json"}); err != nil {
			t.Errorf("api-diff: %v", err)
		}
	})
	var res apiDiffResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if res.Ref != "HEAD" || !res.Breaking {
		t.Fatalf("expected breaking diff vs HEAD: %+v", res)
	}
	joined := strings.Join(res.Added, "\n")
	if !strings.Contains(joined, "api.Fresh") || !strings.Contains(joined, "api.Cfg.Extra") {
		t.Fatalf("added should list Fresh and Cfg.Extra: %v", res.Added)
	}
	if strings.Contains(joined, "unexported") {
		t.Fatalf("unexported symbols must not appear: %v", res.Added)
	}
	removed := strings.Join(res.Removed, "\n")
	if !strings.Contains(removed, "api.Gone") || !strings.Contains(removed, "api.Cfg.Old") {
		t.Fatalf("removed should list Gone and Cfg.Old: %v", res.Removed)
	}
	if len(res.Changed) != 1 || res.Changed[0].Symbol != "api.Sig" {
		t.Fatalf("changed should be exactly api.Sig: %+v", res.Changed)
	}
	if !strings.Contains(res.Changed[0].New, "b string") {
		t.Fatalf("changed entry should carry new signature: %+v", res.Changed[0])
	}
}

func TestAPIDiffCleanTreeNotBreaking(t *testing.T) {
	t.Chdir(t.TempDir())
	writeTempGo(t, ".", "api.go", apiDiffV1)
	initGitFixture(t)

	out := captureStdout(t, func() {
		if err := apiDiffCommand(nil); err != nil {
			t.Errorf("api-diff on clean tree: %v", err)
		}
	})
	if !strings.Contains(out, "0 added, 0 removed, 0 changed (no breaking changes)") {
		t.Fatalf("clean tree should report no changes:\n%s", out)
	}
}

func TestAPIDiffAdditionsOnlyNotBreaking(t *testing.T) {
	t.Chdir(t.TempDir())
	writeTempGo(t, ".", "api.go", apiDiffV1)
	initGitFixture(t)
	writeTempGo(t, ".", "extra.go", "package api\n\n// New is additive.\nfunc New() {}\n")

	out := captureStdout(t, func() {
		if err := apiDiffCommand([]string{"--json"}); err != nil {
			t.Errorf("api-diff: %v", err)
		}
	})
	var res apiDiffResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if res.Breaking {
		t.Fatalf("pure additions must not be breaking: %+v", res)
	}
	if len(res.Added) != 1 || !strings.HasPrefix(res.Added[0], "api.New ") {
		t.Fatalf("added = %v", res.Added)
	}
}

func TestAPIDiffOutsideGitRepo(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("GIT_CEILING_DIRECTORIES", dir)
	writeTempGo(t, ".", "api.go", apiDiffV1)
	if err := apiDiffCommand(nil); err == nil {
		t.Fatal("expected error outside a git repository")
	}
	_ = os.Remove("api.go")
}
