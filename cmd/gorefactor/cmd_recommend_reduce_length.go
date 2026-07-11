package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"

	"github.com/chrisophus/gorefactor/analyzer"
)

// runReduceLength implements `gorefactor recommend --reduce-length <file>
// <Func|Receiver:Method> [--max-lines N] [--json] [--apply [--allow-returns]]`
// — the line-count analog of --reduce-complexity. It finds the minimum set of
// top-level blocks whose extraction brings an over-threshold function under
// the line limit, and with --apply extracts them.
func runReduceLength(args []string) error {
	maxLines := longFunctionThreshold
	jsonOut := false
	apply := false
	allowReturns := false
	var positionals []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--reduce-length":
			// mode flag, consume nothing
		case "--apply":
			apply = true
		case "--allow-returns":
			allowReturns = true
		case "--json":
			jsonOut = true
		case "--function":
			if i+1 < len(args) {
				positionals = append(positionals, args[i+1])
				i++
			}
		case "--max-lines":
			if i+1 >= len(args) {
				return fmt.Errorf("missing value for --max-lines")
			}
			val, err := strconv.Atoi(args[i+1])
			if err != nil {
				return fmt.Errorf("invalid value for --max-lines: %v", err)
			}
			maxLines = val
			i++
		default:
			positionals = append(positionals, args[i])
		}
	}
	if len(positionals) < 2 {
		return fmt.Errorf("usage: recommend --reduce-length <file> <Func|Receiver:Method> [--max-lines N] [--json] [--apply [--allow-returns]]")
	}
	file, function := positionals[0], positionals[1]

	if apply {
		applied, err := reduceLengthByExtraction(file, function, maxLines, allowReturns)
		if err != nil {
			return err
		}
		if applied == 0 {
			fmt.Printf("No blocks extracted from %s — no top-level block the extract engine can lift.\n", function)
			return nil
		}
		fmt.Printf("Extracted %d block(s) from %s to reduce its length.\n", applied, function)
		return nil
	}

	res, err := analyzer.RecommendLengthReduction(file, function, maxLines)
	if err != nil {
		return err
	}
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(res)
	}
	if len(res.Extractions) == 0 {
		fmt.Printf("%s is %d lines (threshold %d) — nothing to extract.\n", res.Function, res.Lines, res.Threshold)
		return nil
	}
	fmt.Printf("%s is %d lines (threshold %d). Suggested extractions (projected %d lines):\n",
		res.Function, res.Lines, res.Threshold, res.Projected)
	for _, e := range res.Extractions {
		fmt.Printf("  lines %d-%d (sheds %d) -> %s\n", e.StartLine, e.EndLine, e.Lines, e.Suggestion)
	}
	fmt.Printf("Apply with: gorefactor recommend --reduce-length %s %s --apply\n", file, function)
	return nil
}

// reduceLengthByExtraction applies the extractions recommended for function in
// file and returns how many blocks were successfully extracted. Blocks are
// applied bottom-up so an earlier extraction never invalidates the line
// numbers of a block still queued. It is shared by the long-function and
// extract-candidate autofixes and `recommend --reduce-length --apply`.
func reduceLengthByExtraction(file, function string, maxLines int, allowReturns bool) (int, error) {
	res, err := analyzer.RecommendLengthReduction(file, function, maxLines)
	if err != nil {
		return 0, err
	}
	ext := append([]analyzer.LengthExtraction(nil), res.Extractions...)
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
	return applied, nil
}
