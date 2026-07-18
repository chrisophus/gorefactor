package analyzer

import (
	"os"
	"strings"
)

// GitRepoEnvVars are git's per-invocation repository-locating environment
// variables. Inherited by child processes of a git hook, they redirect any
// git command run in another directory back to the hook's repository.
var GitRepoEnvVars = []string{
	"GIT_DIR",
	"GIT_INDEX_FILE",
	"GIT_WORK_TREE",
	"GIT_OBJECT_DIRECTORY",
	"GIT_ALTERNATE_OBJECT_DIRECTORIES",
	"GIT_PREFIX",
	"GIT_COMMON_DIR",
	"GIT_QUARANTINE_PATH",
}

// SanitizedGitEnv returns the current environment with GitRepoEnvVars
// removed. Git exports those to hook processes, and any child that runs git
// commands in a different directory — such as a test creating a temporary
// repository under `go test` launched from a pre-commit hook — would
// silently operate on the hook's repository instead of its own. Every gate
// that shells out to `go build` / `go test` uses this so the suite behaves
// identically inside and outside git hooks.
func SanitizedGitEnv() []string {
	drop := map[string]bool{}
	for _, v := range GitRepoEnvVars {
		drop[v] = true
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
