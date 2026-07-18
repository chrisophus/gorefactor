package main

import (
	"os"
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
	os.Exit(m.Run())
}
