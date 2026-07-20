package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/chrisophus/gorefactor/analyzer"
)

// TestMain scrubs git's repository-locating variables before any test runs;
// see the identical guard in cmd/gorefactor for why (tests spawn git in
// temp repositories and must not inherit a hook's GIT_DIR).
func TestMain(m *testing.M) {
	for _, v := range analyzer.GitRepoEnvVars {
		os.Unsetenv(v)
	}
	ensureTestGorefactorOnPATH()
	os.Exit(m.Run())

}

func ensureTestGorefactorOnPATH() {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		return
	}
	bin := filepath.Join(root, "gorefactor")
	if _, err := os.Stat(bin); err != nil {
		cmd := exec.Command("go", "build", "-o", bin, "./cmd/gorefactor")
		cmd.Dir = root
		if err := cmd.Run(); err != nil {
			return
		}
	}
	os.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
}
