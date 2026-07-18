package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/chrisophus/gorefactor/analyzer"
)

// TestMain scrubs git's repository-locating variables before any test runs.
// Many tests here create throwaway git repositories; when the suite runs as
// a child of a git hook (pre-commit runs the doctor gate), inherited GIT_DIR
// and friends would silently point those tests' git commands at the real
// repository — one such run corrupted the main index.
func TestMain(m *testing.M) {
	for _, v := range analyzer.GitRepoEnvVars {
		os.Unsetenv(v)
	}
	os.Exit(m.Run())
}

func writeTempGo(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func fileLineCountOrFail(t *testing.T, path string) int {
	t.Helper()
	n, err := fileLineCount(path)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	digits := []byte{}
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	return string(digits)
}
