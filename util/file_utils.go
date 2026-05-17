package util

import (
	"os"
	"path/filepath"
	"strings"
)

// FindGoFiles recursively finds all Go files in a directory.
// Skips hidden directories (starting with .) and vendor directories.
func FindGoFiles(dirPath string) ([]string, error) {
	var files []string
	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			if strings.HasPrefix(info.Name(), ".") || info.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}

		if strings.HasSuffix(path, ".go") {
			files = append(files, path)
		}
		return nil
	})

	return files, err
}

// WalkGoFiles walks all Go files in a directory and calls fn for each.
// Skips hidden directories and vendor directories.
func WalkGoFiles(dirPath string, fn func(path string) error) error {
	return filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			if strings.HasPrefix(info.Name(), ".") || info.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}

		if strings.HasSuffix(path, ".go") {
			return fn(path)
		}
		return nil
	})
}

// IsGoFile returns true if the path ends with .go
func IsGoFile(path string) bool {
	return strings.HasSuffix(path, ".go")
}
