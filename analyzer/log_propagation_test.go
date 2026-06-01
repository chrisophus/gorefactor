package analyzer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileIfErrLogReturnIssues_DetectsLogThenReturn(t *testing.T) {
	t.Parallel()
	const src = `package p

func f() error {
	if err != nil {
		logger.Error("op", "err", err)
		return err
	}
	return nil
}
`
	issues, err := FileIfErrLogReturnIssues(writeTempGo(t, src))
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 || issues[0].Rule != "if-err-log-return" {
		t.Fatalf("issues = %+v, want one if-err-log-return", issues)
	}
}

func TestShouldSkipFile_SkipFilePaths(t *testing.T) {
	t.Parallel()
	opts := WalkOptions{
		SkipGeneratedGo: true,
		SkipFilePaths:   []string{"internal/data/model.go"},
	}
	if !ShouldSkipFile("internal/data/model.go", opts) {
		t.Fatal("expected model.go skipped")
	}
	if ShouldSkipFile("internal/data/repository.go", opts) {
		t.Fatal("did not expect repository.go skipped")
	}
}

func TestShouldSkipDir_ExtraSegments(t *testing.T) {
	t.Parallel()
	opts := WalkOptions{
		SkipGeneratedGo:      true,
		ExtraSkipDirSegments: []string{"api/marketplace-servergen", "internal/data/db"},
	}
	if !ShouldSkipDir("api/marketplace-servergen", opts) {
		t.Fatal("expected marketplace-servergen skipped")
	}
	if !ShouldSkipDir("internal/data/db", opts) {
		t.Fatal("expected internal/data/db skipped")
	}
}

func writeTempGo(t *testing.T, src string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "x.go")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
