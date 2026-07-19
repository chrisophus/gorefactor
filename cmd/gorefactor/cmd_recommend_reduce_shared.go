package main

import (
	"fmt"
	"os"
	"sort"
	"strconv"

	"github.com/chrisophus/gorefactor/analyzer"
)

// extractionSpec captures the fields analyzer.ComplexityExtraction and
// analyzer.LengthExtraction share: the block boundaries and suggested helper
// name. Reducing both to this common shape lets the complexity- and
// length-reduction autofixes share one apply loop.
type extractionSpec struct {
	StartLine  int
	EndLine    int
	Suggestion string
}

// applyExtractionsBottomUp hands each extraction to the AST-aware `extract`
// engine, bottom-up (highest start line first) so an earlier extraction never
// invalidates the line numbers of a block still queued, and returns how many
// succeeded. Each extractCommand writes only on full success, so a skipped
// block leaves the tree untouched; under `lint --fix --verify` the build+test
// gate still guards the net result and reverts it if it goes red.
func applyExtractionsBottomUp(file string, extractions []extractionSpec, allowReturns bool) int {
	ext := append([]extractionSpec(nil), extractions...)
	sort.SliceStable(ext, func(i, j int) bool { return ext[i].StartLine > ext[j].StartLine })

	applied := 0
	for _, e := range ext {
		args := []string{file, strconv.Itoa(e.StartLine), strconv.Itoa(e.EndLine), e.Suggestion}
		if allowReturns {
			args = append(args, "--allow-returns")
		}
		if err := extractCommand(args); err != nil {
			fmt.Fprintf(os.Stderr, "  skip block L%d-%d: %v\n", e.StartLine, e.EndLine, err)
			continue
		}
		applied++
	}
	return applied
}

func applyNameableExtractions[T any](file string, exs []T, allowReturns bool, project func(T) extractionSpec) int {
	var specs []extractionSpec
	for _, e := range exs {
		s := project(e)
		if analyzer.IsGeneratedFallbackName(s.Suggestion) {
			continue
		}
		specs = append(specs, s)
	}
	return applyExtractionsBottomUp(file, specs, allowReturns)
}

// reduceFlags holds the flags shared between `recommend --reduce-complexity`
// and `recommend --reduce-length`, which differ only in the name of the mode
// flag and the name/meaning of the numeric threshold flag.
type reduceFlags struct {
	positionals  []string
	apply        bool
	allowReturns bool
	jsonOut      bool
	numeric      int // threshold or maxLines, whichever the caller asked for
}

// parseReduceFlags parses the flags common to both reduce modes. modeFlag is
// the mode-selecting flag to skip silently (e.g. "--reduce-complexity");
// numericFlag is the flag name for the threshold/max-lines value (e.g.
// "--threshold", "--max-lines"); def is its default when unset.
func parseReduceFlags(args []string, modeFlag, numericFlag string, def int) (reduceFlags, error) {
	rf := reduceFlags{numeric: def}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case modeFlag:
			// mode flag, consume nothing
		case "--apply":
			rf.apply = true
		case "--allow-returns":
			rf.allowReturns = true
		case "--json":
			rf.jsonOut = true
		case "--function":
			if i+1 < len(args) {
				rf.positionals = append(rf.positionals, args[i+1])
				i++
			}
		case numericFlag:
			if i+1 >= len(args) {
				return rf, fmt.Errorf("missing value for %s", numericFlag)
			}
			val, err := strconv.Atoi(args[i+1])
			if err != nil {
				return rf, fmt.Errorf("invalid value for %s: %v", numericFlag, err)
			}
			rf.numeric = val
			i++
		default:
			rf.positionals = append(rf.positionals, args[i])
		}
	}
	return rf, nil
}
