package analyzer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestShouldSkipFile_GeneratedSuffixes(t *testing.T) {
	t.Parallel()
	opts := DefaultWalkOptions()
	cases := []struct {
		path string
		want bool
	}{
		{"main.go", false},
		{"foo.gen.go", true},
		{"foo_gen.go", true},
		{"handler.go", false},
	}
	for _, tc := range cases {
		if got := ShouldSkipFile(tc.path, opts); got != tc.want {
			t.Errorf("ShouldSkipFile(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestShouldSkipDir_ExtraSegment(t *testing.T) {
	t.Parallel()
	opts := WalkOptions{
		SkipGeneratedGo:      true,
		ExtraSkipDirSegments: []string{"api/marketplace-servergen"},
	}
	if !ShouldSkipDir("api/marketplace-servergen", opts) {
		t.Error("expected generated API tree dir to be skipped")
	}
	if ShouldSkipDir("api/endpoints", opts) {
		t.Error("did not expect api/endpoints to be skipped")
	}
}

func TestWalkGoFiles_SkipsVendorAndGenerated(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "vendor"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(tmp, "main.go"), "package main\n")
	mustWrite(t, filepath.Join(tmp, "types.gen.go"), "package main\n")
	mustWrite(t, filepath.Join(tmp, "vendor", "dep.go"), "package dep\n")

	files, err := WalkGoFiles(tmp, DefaultWalkOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || filepath.Base(files[0]) != "main.go" {
		t.Fatalf("expected only main.go; got %v", files)
	}
}

func TestGroupFilesByDir(t *testing.T) {
	t.Parallel()
	got := GroupFilesByDir([]string{"/a/x.go", "/a/y.go", "/b/z.go"})
	if len(got["/a"]) != 2 || len(got["/b"]) != 1 {
		t.Fatalf("unexpected grouping: %v", got)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
