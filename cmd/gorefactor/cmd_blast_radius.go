package main

import (
	"fmt"
	"go/ast"
	"path/filepath"
	"sort"
	"strings"

	"github.com/chrisophus/gorefactor/analyzer"
)

var blastRadiusFlags = map[string]bool{
	"--json": false,
	"--in":   true,
}

func init() {
	registerCommand(Command{
		Name:        "blast-radius",
		ReadOnly:    true,
		MCPTool:     true,
		Description: "Score the change-impact (blast radius) of a function/method by its transitive callers [--in path] [--json]",
		Usage:       "blast-radius <Func|Receiver:Method> [--in path] [--json]",
		MinArgs:     1,
		MaxArgs:     1,
		Flags:       blastRadiusFlags,
		Run:         blastRadiusCommand,
	})
}

// blastRadius // blastRadius is the change-impact report for one function/method: how far a //
// change to it can ripple, measured over the reverse call graph. Larger numbers // mean a change
// here is riskier to make. It is the function-level analog of the // package fan-in signal in the
// high-coupling rule. // // Counts are an over-approximation: like callgraph/find-callers, call
// edges are // resolved by name (a selector call x.Foo() matches Foo on any receiver), so // shared
// method names inflate the reach. Treat the score as a ranking signal, // not an exact dependency
// count.
type blastRadius struct {
	Target            string   `json:"target"`
	Exported          bool     `json:"exported"`
	DirectCallers     int      `json:"directCallers"`
	TransitiveCallers int      `json:"transitiveCallers"`
	FilesAffected     int      `json:"filesAffected"`
	PackagesAffected  int      `json:"packagesAffected"`
	Score             int      `json:"score"`
	Level             string   `json:"level"`
	TopCallers        []string `json:"topCallers,omitempty"`
}

// computeBlastRadius walks the reverse call edges from def to find every
// function that transitively reaches it, then scores the result. The
// transitive-caller count dominates (each reachable caller is a place a change
// can break); spanning many distinct files and being part of the exported API
// add to the score because they widen who is affected.
func computeBlastRadius(idx *cgIndex, def *cgDef) blastRadius {
	closure := idx.TransitiveCallers(def)

	files := map[string]bool{}
	pkgs := map[string]bool{}
	for _, c := range closure {
		files[c.File] = true
		pkgs[filepath.Dir(c.File)] = true
	}

	exported := ast.IsExported(def.Name)
	// Weights are deliberately simple and monotonic so the score stays
	// human-readable: transitive reach is the dominant term, breadth across
	// files/packages adds a little, and exported symbols carry an API premium.
	score := len(closure)*2 + len(files) + len(pkgs)
	if exported {
		score += 5
	}

	return blastRadius{
		Target:            def.Key(),
		Exported:          exported,
		DirectCallers:     len(idx.Callers[def.Key()]),
		TransitiveCallers: len(closure),
		FilesAffected:     len(files),
		PackagesAffected:  len(pkgs),
		Score:             score,
		Level:             blastLevel(score),
		TopCallers:        topCallerKeys(idx.Callers[def.Key()]),
	}
}

// blastLevel buckets a score into a human label. Thresholds are advisory; the
// numeric score is the rankable signal.
func blastLevel(score int) string {
	switch {
	case score >= 25:
		return "high"
	case score >= 8:
		return "medium"
	default:
		return "low"
	}
}

func topCallerKeys(callers []*cgDef) []string {
	keys := make([]string, 0, len(callers))
	for _, c := range callers {
		keys = append(keys, c.Key())
	}
	sort.Strings(keys)
	if len(keys) > 8 {
		keys = keys[:8]
	}
	return keys
}

func blastRadiusCommand(args []string) error {
	positional, flags := parseFlags(args, blastRadiusFlags)
	target := positional[0]
	root := "."
	if flags["--in"] != "" {
		root = flags["--in"]
	}

	files, err := collectGoFiles(root, analyzer.DefaultWalkOptions())
	if err != nil {
		return err
	}
	idx, err := buildCallIndex(files)
	if err != nil {
		return err
	}

	def, err := idx.LookupTargetOrSuggest(target)
	if err != nil {
		return err
	}

	br := computeBlastRadius(idx, def)

	if flags["--json"] != "" {
		emitJSON(br)
		return nil
	}

	fmt.Printf("blast radius of %s\n", br.Target)
	fmt.Printf("  exported:           %v\n", br.Exported)
	fmt.Printf("  direct callers:     %d\n", br.DirectCallers)
	fmt.Printf("  transitive callers: %d\n", br.TransitiveCallers)
	fmt.Printf("  files affected:     %d\n", br.FilesAffected)
	fmt.Printf("  packages affected:  %d\n", br.PackagesAffected)
	fmt.Printf("  score:              %d  (level: %s)\n", br.Score, br.Level)
	if len(br.TopCallers) > 0 {
		fmt.Printf("  direct callers: %s\n", strings.Join(br.TopCallers, ", "))
	}
	return nil
}
