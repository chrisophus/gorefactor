package analyzer

import (
	"os"
	"strings"
)

// SanitizedGitEnv returns the current environment with git's per-invocation
// repository-locating variables removed (GIT_DIR, GIT_INDEX_FILE,
// GIT_WORK_TREE, ...). Git exports these to hook processes, and any child
// that runs git commands in a different directory — such as a test creating
// a temporary repository under `go test` launched from a pre-commit hook —
// would silently operate on the hook's repository instead of its own. Every
// gate that shells out to `go build` / `go test` uses this so the suite
// behaves identically inside and outside git hooks.
func SanitizedGitEnv() []string {
	drop := map[string]bool{
		"GIT_DIR":                          true,
		"GIT_INDEX_FILE":                   true,
		"GIT_WORK_TREE":                    true,
		"GIT_OBJECT_DIRECTORY":             true,
		"GIT_ALTERNATE_OBJECT_DIRECTORIES": true,
		"GIT_PREFIX":                       true,
		"GIT_COMMON_DIR":                   true,
		"GIT_QUARANTINE_PATH":              true,
	}
	env := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if drop[name] {
			continue
		}
		env = append(env, kv)
	}
	return env
}
