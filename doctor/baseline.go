package doctor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/chrisophus/gorefactor/analyzer"
	"github.com/chrisophus/gorefactor/config"
)

// baselineRecord is one fingerprint's baseline entry: how many matching
// findings existed at the base ref, and at what severity (for FixedCount).
type baselineRecord struct {
	Count    int      `json:"count"`
	Severity Severity `json:"severity"`
}

// BaselineSet is the base ref's findings keyed by fingerprint. It is computed
// once per base commit and cached keyed by its SHA (plan decision 9), so the
// per-edit hot path analyzes one tree, never two.
type BaselineSet map[string]baselineRecord

type baselineFile struct {
	Version int                       `json:"version"`
	SHA     string                    `json:"sha"`
	Records map[string]baselineRecord `json:"records"`
}

// LoadOrBuildBaseline returns the baseline fingerprint set for baseSHA,
// building it in a detached git worktree on cache miss. Diff-based substrates
// are excluded (their findings are relative to the base by construction), and
// a substrate that is unavailable at the base contributes nothing.
func LoadOrBuildBaseline(root, baseSHA string, substrates []Substrate) (BaselineSet, error) {
	cachePath := baselineCachePath(root, baseSHA)
	if data, err := os.ReadFile(cachePath); err == nil {
		var bf baselineFile
		if jerr := json.Unmarshal(data, &bf); jerr == nil && bf.SHA == baseSHA {
			return bf.Records, nil
		}
	}

	set, err := buildBaseline(root, baseSHA, substrates)
	if err != nil {
		return nil, fmt.Errorf("build baseline: %w", err)
	}
	saveBaselineCache(cachePath, baseSHA, set)
	return set, nil
}

func baselineCachePath(root, sha string) string {
	short := sha
	if len(short) > 12 {
		short = short[:12]
	}
	return filepath.Join(root, ".gorefactor", "doctor-baseline-"+short+".json")
}

// resolveSHA resolves ref to a commit SHA in root's repository.
func resolveSHA(root, ref string) (string, error) {
	out, err := gitCmd(root, "rev-parse", "--verify", ref+"^{commit}").Output()

	if err != nil {
		return "", fmt.Errorf("resolve ref %q: %w", ref, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func buildBaseline(root, baseSHA string, substrates []Substrate) (BaselineSet, error) {
	tmp, err := os.MkdirTemp("", "gorefactor-doctor-base-")
	if err != nil {
		return nil, fmt.Errorf("baseline temp dir: %w", err)
	}
	worktree := filepath.Join(tmp, "tree")
	if out, werr := gitCmd(root, "worktree", "add", "--detach", worktree, baseSHA).CombinedOutput(); werr != nil {
		os.RemoveAll(tmp)
		return nil, fmt.Errorf("baseline worktree for %s: %v: %s", baseSHA, werr, strings.TrimSpace(string(out)))
	}
	defer func() {
		_ = gitCmd(root, "worktree", "remove", "--force", worktree).Run()
		os.RemoveAll(tmp)
	}()

	walk := analyzer.DefaultWalkOptions()
	if cfg, cerr := config.Load("", worktree); cerr == nil && cfg != nil {
		walk = cfg.WalkOptions()
	}

	set := BaselineSet{}
	for _, s := range substrates {
		info := s.Info()
		if info.DiffBased {
			continue
		}
		findings, rerr := s.Run(RunContext{Root: worktree})
		if rerr != nil {
			continue // unavailable or broken at the base: contributes nothing
		}
		for i := range findings {
			fillDefaults(&findings[i], info.Name)
		}
		for _, f := range normalizeFindings(findings, worktree, walk) {
			rec := set[f.Fingerprint]
			rec.Count++
			rec.Severity = f.Severity
			set[f.Fingerprint] = rec
		}
	}
	return set, nil
}

func saveBaselineCache(path, sha string, set BaselineSet) {
	data, err := json.Marshal(baselineFile{Version: 1, SHA: sha, Records: set})
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644) // cache is best-effort
}

// markNew flags each finding not covered by the baseline. Count-aware, like
// the lint ratchet: the first baseline-count occurrences of a fingerprint are
// pre-existing; occurrences beyond that are new. Callers sort findings first
// so which occurrence is flagged stays deterministic.
func markNew(findings []Finding, base BaselineSet, diffBased map[string]bool) {
	seen := map[string]int{}
	for i := range findings {
		if diffBased[findings[i].Substrate] {
			continue // New set by the substrate itself
		}
		fp := findings[i].Fingerprint
		seen[fp]++
		findings[i].New = seen[fp] > base[fp].Count
	}
}
