package orchestrator

import (
	"fmt"
	"os"

	"golang.org/x/tools/imports"
)

// FormatImports is the exported version of formatImports for use by callers
// outside the orchestrator package (e.g. the top-level format command).
func FormatImports(path string) error {
	return formatImports(path)
}

// formatImports runs goimports-equivalent logic in-process on the given file,
// adding missing imports and removing unused ones. Replaces the previous shell-out
// to the goimports binary so gorefactor doesn't silently produce broken code when
// goimports isn't installed on PATH.
func formatImports(path string) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", path, err)
	}
	out, err := imports.Process(path, src, &imports.Options{
		Comments:   true,
		TabIndent:  true,
		TabWidth:   8,
		FormatOnly: false,
	})
	if err != nil {
		return fmt.Errorf("failed to process imports for %s: %w", path, err)
	}
	if err := os.WriteFile(path, out, 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}
	return nil
}
