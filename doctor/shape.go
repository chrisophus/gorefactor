package doctor

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/modfile"

	"github.com/chrisophus/gorefactor/analyzer"
)

// Shape is the project-shape sniff from the design plan: doctor selects rules
// and conditions severity on what kind of module this is, the analog of
// react-doctor detecting Next vs Vite vs React Native.
type Shape struct {
	// ModulePath from go.mod.
	ModulePath string
	// GoVersion from go.mod (e.g. "1.26").
	GoVersion string
	// MainDirs are package-main directories relative to the root. Empty means
	// a pure library module.
	MainDirs []string
	// IsLibrary is true when the module has no main packages: os.Exit or
	// log.Fatal is a finding here, not in a binary's main package.
	IsLibrary bool
	// HasTemporal is true when the module requires go.temporal.io/sdk —
	// enables the workflow-determinism substrate.
	HasTemporal bool
	// HasKafka is true when a Kafka client is required; reserved for future
	// consumer-hygiene rules.
	HasKafka bool
}

// kafkaModules are the Kafka client modules the shape sniff recognizes.
var kafkaModules = []string{
	"github.com/segmentio/kafka-go",
	"github.com/confluentinc/confluent-kafka-go",
	"github.com/IBM/sarama",
	"github.com/Shopify/sarama",
}

// DetectShape sniffs the module at root: go.mod requirements plus a
// package-clause-only scan for main packages.
func DetectShape(root string) (*Shape, error) {
	shape := &Shape{}
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return nil, fmt.Errorf("read go.mod: %w", err)
	}
	mod, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		return nil, fmt.Errorf("parse go.mod: %w", err)
	}
	if mod.Module != nil {
		shape.ModulePath = mod.Module.Mod.Path
	}
	if mod.Go != nil {
		shape.GoVersion = mod.Go.Version
	}
	for _, req := range mod.Require {
		path := req.Mod.Path
		if path == "go.temporal.io/sdk" {
			shape.HasTemporal = true
		}
		for _, k := range kafkaModules {
			if path == k {
				shape.HasKafka = true
			}
		}
	}
	if shape.MainDirs, err = findMainDirs(root); err != nil {
		return nil, fmt.Errorf("find main dirs: %w", err)
	}
	shape.IsLibrary = len(shape.MainDirs) == 0
	return shape, nil
}

// findMainDirs lists directories containing a main package, via
// package-clause-only parsing (no full AST work).
func findMainDirs(root string) ([]string, error) {
	files, err := analyzer.WalkGoFiles(root, analyzer.DefaultWalkOptions())
	if err != nil {
		return nil, fmt.Errorf("walk: %w", err)
	}
	seen := map[string]bool{}
	var dirs []string
	fset := token.NewFileSet()
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		dir := filepath.Dir(f)
		if seen[dir] {
			continue
		}
		astFile, perr := parser.ParseFile(fset, f, nil, parser.PackageClauseOnly)
		if perr != nil || astFile.Name.Name != "main" {
			continue
		}
		seen[dir] = true
		rel, rerr := filepath.Rel(root, dir)
		if rerr != nil {
			rel = dir
		}
		dirs = append(dirs, filepath.ToSlash(rel))
	}
	return dirs, nil
}
