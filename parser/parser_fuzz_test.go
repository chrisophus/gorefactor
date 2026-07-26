package parser

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// FuzzParseFile throws arbitrary source at ParseFile. The contract under
// fuzz: never panic, and on success return a FileInfo that survives the JSON
// round-trip this package exists to produce. Seeds cover the constructs the
// walker branches on, including the parseable-but-invalid empty receiver
// that mutation testing showed is easy to mishandle.
func FuzzParseFile(f *testing.F) {
	seeds := []string{
		"package p\n",
		"package p\n\nfunc F(a int) (b string) { return \"\" }\n",
		"package p\n\ntype S struct{ X, Y int }\n\nfunc (s *S) M() {}\n",
		"package p\n\ntype I interface {\n\tRead(p []byte) (int, error)\n\tEmbedded\n}\n",
		"package p\n\nfunc () orphan() {}\n",
		"package p\n\nfunc G[T any](x T) T { return x }\n",
		"package p\n\ntype C interface{ ~int | ~string }\n",
		"package p\n\nimport (\n\t\"fmt\"\n)\n\nvar _ = fmt.Sprint\n",
		"package p\n\nfunc Stub() int\n",
		"pack age p\nfunc {",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		path := filepath.Join(t.TempDir(), "fuzz.go")
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		info, err := ParseFile(path)
		if err != nil {
			if info != nil {
				t.Errorf("ParseFile returned both a FileInfo and an error")
			}
			return
		}
		if info == nil {
			t.Fatalf("ParseFile returned (nil, nil)")
		}
		if _, err := json.Marshal(info); err != nil {
			t.Errorf("FileInfo not JSON-marshalable: %v", err)
		}
	})
}
