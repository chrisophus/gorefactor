package main

import (
	"os"
	"path/filepath"
)

func glob(root, dir, pattern string) []string {
	var files []string
	base := filepath.Join(root, dir)
	if pattern == "**/*.go" {
		filepath.WalkDir(root, func(path string, d os.DirEntry, _ error) error {
			if d == nil || d.IsDir() {
				return nil
			}
			if filepath.Ext(path) == ".go" {
				rel, _ := filepath.Rel(root, path)
				files = append(files, rel)
			}
			return nil
		})
		return files
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		match, _ := filepath.Match(pattern, e.Name())
		if match {
			rel, _ := filepath.Rel(root, filepath.Join(base, e.Name()))
			files = append(files, rel)
		}
	}
	return files
}

func fileSize(root, rel string) int {
	data, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		return 0
	}
	return len(data)
}
