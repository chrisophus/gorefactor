package version

import (
	"strings"
	"testing"
)

// A local build reports "(devel)"; release builds report the injected tag or
// the module version. Either way the result must be non-empty and stable.
func TestVersionNonEmptyAndStable(t *testing.T) {
	v := Version()
	if strings.TrimSpace(v) == "" {
		t.Fatal("Version() returned an empty string")
	}
	if v2 := Version(); v2 != v {
		t.Fatalf("Version() not stable across calls: %q then %q", v, v2)
	}
}

// The GoReleaser-injected tag must win over build info so release binaries
// always report the exact tag.
func TestVersionInjectedTakesPriority(t *testing.T) {
	old := injected
	defer func() { injected = old }()
	injected = "v9.9.9-test"
	if got := Version(); got != "v9.9.9-test" {
		t.Fatalf("Version() = %q, want the injected tag", got)
	}
}
