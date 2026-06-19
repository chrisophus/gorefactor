package main

import (
	"github.com/chrisophus/gorefactor/analyzer"
)

// Parse optional arguments

// Find all Go files

// Analyze each file

// Log but don't fail

// Output results

// Text format (linter-style)

// Show extraction hints

// Summary

func findGoFiles(directory string) ([]string, error) {
	return analyzer.WalkGoFiles(directory, analyzer.DefaultWalkOptions())
}
