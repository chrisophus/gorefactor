package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

const sampleGo = `package sample

func Sum(nums []int) int {
	total := 0
	for i := 0; i < len(nums); i++ {
		total = total + nums[i]
	}
	return total
}
`

const sampleTestGo = `package sample

import "testing"

func TestSum(t *testing.T) {
	if Sum([]int{1, 2, 3}) != 6 {
		t.Fatalf("Sum wrong")
	}
}
`

func newSampleRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module sample\n\ngo 1.21\n")
	write("sample.go", sampleGo)
	write("sample_test.go", sampleTestGo)
	// Mirror the real repo: .gorefactor/ is gitignored, so the agent's
	// rollback (git clean -fd, no -x) preserves the persistent notes and
	// failure corpus across attempts.
	write(".gitignore", ".gorefactor/\n")

	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "t"},
		{"config", "commit.gpgsign", "false"},
		{"add", "-A"},
		{"commit", "-q", "-m", "init"},
	} {
		c := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}
