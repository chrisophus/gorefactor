package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gorefactor/orchestrator"
)

func formatCommand(args []string) error {
	targets := args
	if len(targets) == 0 {
		targets = []string{"."}
	}
	var files []string
	for _, t := range targets {
		info, err := os.Stat(t)
		if err != nil {
			return fmt.Errorf("stat %s: %w", t, err)
		}
		if info.IsDir() {
			err := filepath.Walk(t, func(path string, fi os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				if fi.IsDir() {
					name := fi.Name()
					if name == "vendor" || name == ".git" || name == ".gorefactor" {
						return filepath.SkipDir
					}
					return nil
				}
				if strings.HasSuffix(path, ".go") {
					files = append(files, path)
				}
				return nil
			})
			if err != nil {
				return err
			}
		} else if strings.HasSuffix(t, ".go") {
			files = append(files, t)
		}
	}
	for _, f := range files {
		if err := orchestrator.FormatImports(f); err != nil {
			fmt.Fprintf(os.Stderr, "warning: %s: %v\n", f, err)
		}
	}
	fmt.Printf("Formatted %d files\n", len(files))
	return nil
}
