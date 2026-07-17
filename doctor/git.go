package doctor

import (
	"os"
	"os/exec"
	"strings"
)

// gitCmd builds a git command targeted at root via -C, with the environment
// scrubbed of repo-targeting GIT_* variables. Under a git hook (the pre-commit
// doctor gate, an agent gate run inside a commit) git exports GIT_INDEX_FILE /
// GIT_DIR pointing at the outer repository; inherited by our subprocesses they
// redirect worktree, rev-parse, and diff calls at the wrong repo.
func gitCmd(root string, args ...string) *exec.Cmd {
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	cmd.Env = gitScrubbedEnv()
	return cmd
}

func gitScrubbedEnv() []string {
	drop := map[string]bool{
		"GIT_DIR": true, "GIT_INDEX_FILE": true, "GIT_WORK_TREE": true,
		"GIT_OBJECT_DIRECTORY": true, "GIT_COMMON_DIR": true, "GIT_PREFIX": true,
	}
	env := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if !drop[name] {
			env = append(env, kv)
		}
	}
	return env
}
