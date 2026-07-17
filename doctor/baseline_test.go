package doctor

import (
	"testing"
)

func TestMarkNewCountAware(t *testing.T) {
	base := BaselineSet{
		fingerprint(Finding{File: "a.go", Rule: "dup", Message: "m"}): {Count: 1, Severity: SeverityWarning},
	}
	findings := []Finding{
		{File: "a.go", Rule: "dup", Message: "m"},
		{File: "a.go", Rule: "dup", Message: "m"}, // second occurrence: beyond baseline count
		{File: "b.go", Rule: "dup", Message: "m"}, // unseen file
	}
	for i := range findings {
		findings[i].Fingerprint = fingerprint(findings[i])
	}
	markNew(findings, base, nil)
	if findings[0].New || !findings[1].New || !findings[2].New {
		t.Fatalf("count-aware marking wrong: %+v", findings)
	}
}

func TestMarkNewSkipsDiffBasedSubstrates(t *testing.T) {
	findings := []Finding{{Substrate: "apidiff", Rule: "api-removed", Message: "m", New: true}}
	findings[0].Fingerprint = fingerprint(findings[0])
	markNew(findings, BaselineSet{}, map[string]bool{"apidiff": true})
	if !findings[0].New {
		t.Fatal("diff-based findings keep the New the substrate set")
	}
}

func TestLoadOrBuildBaselineCaches(t *testing.T) {
	root := gitRepo(t, map[string]string{"a.go": "package a\n"})
	sha, err := resolveSHA(root, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	sub := &fakeSubstrate{
		info: SubstrateInfo{Name: "fake"},
		findings: []Finding{{
			File: "a.go", Rule: "r", Category: CategoryStruct, Message: "m at line 3",
		}},
	}
	set, err := LoadOrBuildBaseline(root, sha, []Substrate{sub})
	if err != nil {
		t.Fatal(err)
	}
	if len(set) != 1 {
		t.Fatalf("baseline should hold 1 fingerprint: %v", set)
	}
	// Second load must come from the cache: a substrate that now errors
	// would poison a rebuild but not a cache hit.
	sub.err = ErrUnavailable
	cached, err := LoadOrBuildBaseline(root, sha, []Substrate{sub})
	if err != nil || len(cached) != 1 {
		t.Fatalf("expected cache hit with 1 fingerprint, got %v (%v)", cached, err)
	}
}
