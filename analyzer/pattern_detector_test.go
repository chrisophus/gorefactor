package analyzer

import (
	"go/parser"
	"go/token"
	"testing"
)

func TestDetectLargeClass(t *testing.T) {
	tests := []struct {
		name          string
		code          string
		shouldDetect  bool
		expectedSmell string
	}{
		{
			name: "pure-data struct does not trip Large Class",
			code: `
package main

type Person struct {
	Name string
	Age int
	Email string
	Phone string
	Address string
	City string
	State string
	Zip string
	Country string
	Latitude float64
	Longitude float64
	Bio string
	Website string
	Twitter string
	GitHub string
	LinkedIn string
}
`,
			shouldDetect: false,
		},
		{
			// Trips via methodCount > 20 branch — kept compact compared
			// to the 31-distinct-fields alternative.
			name: "struct with many methods trips Large Class",
			code: `
package main

type Service struct { name string }

func (s *Service) M01() {}
func (s *Service) M02() {}
func (s *Service) M03() {}
func (s *Service) M04() {}
func (s *Service) M05() {}
func (s *Service) M06() {}
func (s *Service) M07() {}
func (s *Service) M08() {}
func (s *Service) M09() {}
func (s *Service) M10() {}
func (s *Service) M11() {}
func (s *Service) M12() {}
func (s *Service) M13() {}
func (s *Service) M14() {}
func (s *Service) M15() {}
func (s *Service) M16() {}
func (s *Service) M17() {}
func (s *Service) M18() {}
func (s *Service) M19() {}
func (s *Service) M20() {}
func (s *Service) M21() {}
`,
			shouldDetect:  true,
			expectedSmell: "Large Class",
		},
		{
			name: "struct with few fields",
			code: `
package main

type Point struct {
	X int
	Y int
}
`,
			shouldDetect: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "test.go", tt.code, 0)
			if err != nil {
				t.Fatalf("failed to parse: %v", err)
			}

			detector := NewPatternDetector(f)
			patterns := detector.detectLargeClass()

			if tt.shouldDetect && len(patterns) == 0 {
				t.Errorf("expected to detect %s, but got none", tt.expectedSmell)
			}
			if !tt.shouldDetect && len(patterns) > 0 {
				t.Errorf("expected no smells, but got %d", len(patterns))
			}
		})
	}
}

func TestDetectDataClumps(t *testing.T) {
	tests := []struct {
		name         string
		code         string
		shouldDetect bool
	}{
		{
			name: "repeated parameter groups",
			code: `
package main

func Validate(email string, phone string, name string) {}
func Process(email string, phone string, name string) {}
func Store(email string, phone string, name string) {}
`,
			shouldDetect: true,
		},
		{
			name: "unique parameter groups",
			code: `
package main

func Validate(email string) {}
func Process(id int) {}
`,
			shouldDetect: false,
		},
		{
			name: "reordered group still matches (order-normalized)",
			code: `
package main

func Validate(name string, email string, phone string) {}
func Store(email string, phone string, name string) {}
`,
			shouldDetect: true,
		},
		{
			name: "same names different types are not conflated",
			code: `
package main

func A(x int, y int, z int) {}
func B(x string, y string, z string) {}
`,
			shouldDetect: false,
		},
		{
			// Idiomatic AST-processing signatures: *token.FileSet threads with
			// *ast.File / ast.Node by necessity. Once carriers are excluded only
			// one real param (src) remains, so this is not a bundleable clump.
			name: "carrier types (fset+ast) are not a data clump",
			code: `
package main

import (
	"go/ast"
	"go/token"
)

func A(fset *token.FileSet, node *ast.File, src []byte) {}
func B(fset *token.FileSet, node *ast.File, src []byte) {}
`,
			shouldDetect: false,
		},
		{
			// *testing.T is not domain data; a test helper group carrying it plus
			// one or two real params must not be flagged (bundling *testing.T
			// into a struct is anti-idiomatic).
			name: "testing.T helper group is not a data clump",
			code: `
package main

import "testing"

func A(t *testing.T, dir string, mode int) {}
func B(t *testing.T, dir string, mode int) {}
`,
			shouldDetect: false,
		},
		{
			// Carriers plus THREE real domain params is still a genuine clump.
			name: "three real params beside a carrier still clumps",
			code: `
package main

import "go/token"

func A(fset *token.FileSet, host string, port string, scheme string) {}
func B(fset *token.FileSet, host string, port string, scheme string) {}
`,
			shouldDetect: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "test.go", tt.code, 0)
			if err != nil {
				t.Fatalf("failed to parse: %v", err)
			}

			detector := NewPatternDetector(f)
			patterns := detector.detectDataClumps()

			if tt.shouldDetect && len(patterns) == 0 {
				t.Errorf("expected to detect data clumps, but got none")
			}
			if !tt.shouldDetect && len(patterns) > 0 {
				t.Errorf("expected no data clumps, but got %d", len(patterns))
			}
		})
	}
}

func TestDetectSwitchStatements(t *testing.T) {
	tests := []struct {
		name         string
		code         string
		shouldDetect bool
	}{
		{
			name: "two functions with one type switch each (idiomatic Go, no smell)",
			code: `
package main

func Validate(t interface{}) {
	switch t.(type) {
	case string:
	case int:
	}
}

func Process(t interface{}) {
	switch t.(type) {
	case string:
	case int:
	}
}
`,
			shouldDetect: false,
		},
		{
			name: "three+ functions, one with multiple type switches (real smell)",
			code: `
package main

func A(t interface{}) {
	switch t.(type) { case string: }
	switch t.(type) { case int: }
}
func B(t interface{}) {
	switch t.(type) { case string: }
}
func C(t interface{}) {
	switch t.(type) { case string: }
}
`,
			shouldDetect: true,
		},
		{
			name: "plain (non-type) switches are not flagged",
			code: `
package main

func A(n int) {
	switch n {
	case 1:
	case 2:
	}
}
func B(n int) {
	switch n {
	case 1:
	case 2:
	}
}
func C(n int) {
	switch n {
	case 1:
	case 2:
	}
}
`,
			shouldDetect: false,
		},
		{
			name: "single function with switch",
			code: `
package main

func Validate(t interface{}) {
	switch t.(type) {
	case string:
	case int:
	}
}
`,
			shouldDetect: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "test.go", tt.code, 0)
			if err != nil {
				t.Fatalf("failed to parse: %v", err)
			}

			detector := NewPatternDetector(f)
			patterns := detector.detectSwitchStatements()

			if tt.shouldDetect && len(patterns) == 0 {
				t.Errorf("expected to detect scattered switches, but got none")
			}
			if !tt.shouldDetect && len(patterns) > 0 {
				t.Errorf("expected no scattered switches, but got %d", len(patterns))
			}
		})
	}
}

func TestDetectPatterns(t *testing.T) {
	// Fixture exercises the post-tuning rules:
	//   - Config trips Large Class via methodCount>20.
	//   - email/phone/name recurs across 2+ funcs (Data Clumps).
	//   - 3 type-switching funcs, one with 2 type switches (Type Switches).
	code := `
package main

type Config struct { host string }

func (c *Config) M01() {}
func (c *Config) M02() {}
func (c *Config) M03() {}
func (c *Config) M04() {}
func (c *Config) M05() {}
func (c *Config) M06() {}
func (c *Config) M07() {}
func (c *Config) M08() {}
func (c *Config) M09() {}
func (c *Config) M10() {}
func (c *Config) M11() {}
func (c *Config) M12() {}
func (c *Config) M13() {}
func (c *Config) M14() {}
func (c *Config) M15() {}
func (c *Config) M16() {}
func (c *Config) M17() {}
func (c *Config) M18() {}
func (c *Config) M19() {}
func (c *Config) M20() {}
func (c *Config) M21() {}

func ProcessRequest(email string, phone string, name string) {}
func StoreUser(email string, phone string, name string) {}

func HandleEvent(e interface{}) {
	switch e.(type) { case string: }
	switch e.(type) { case int: }
}

func HandleMessage(m interface{}) {
	switch m.(type) { case int: }
}

func HandleAck(a interface{}) {
	switch a.(type) { case bool: }
}
`

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "test.go", code, 0)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	detector := NewPatternDetector(f)
	patterns := detector.DetectPatterns()

	if len(patterns) < 2 {
		t.Errorf("expected multiple patterns, but got %d", len(patterns))
	}

	smellNames := make(map[string]bool)
	for _, p := range patterns {
		smellNames[p.Name] = true
	}

	if !smellNames["Large Class"] {
		t.Errorf("expected to detect Large Class")
	}
	if !smellNames["Data Clumps"] {
		t.Errorf("expected to detect Data Clumps")
	}
	if !smellNames["Type Switches"] {
		t.Errorf("expected to detect Type Switches")
	}
}
