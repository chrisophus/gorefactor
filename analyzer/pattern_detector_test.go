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
			name: "struct with many fields",
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
			name: "multiple functions with switches",
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
			shouldDetect: true,
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
	code := `
package main

type Config struct {
	Host string
	Port int
	User string
	Pass string
	DB string
	Timeout int
	MaxConn int
	MinConn int
	SSL bool
	TLS bool
	CertPath string
	KeyPath string
	CAPath string
	CACert string
	ClientCert string
	ClientKey string
}

func ProcessRequest(email string, phone string, name string) {}
func StoreUser(email string, phone string, name string) {}

func HandleEvent(e interface{}) {
	switch e.(type) {
	case string:
	}
}

func HandleMessage(m interface{}) {
	switch m.(type) {
	case int:
	}
}
`

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "test.go", code, 0)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	detector := NewPatternDetector(f)
	patterns := detector.DetectPatterns()

	// Should detect multiple smell types
	if len(patterns) < 2 {
		t.Errorf("expected multiple patterns, but got %d", len(patterns))
	}

	// Verify we got expected smells
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
	if !smellNames["Switch Statements"] {
		t.Errorf("expected to detect Switch Statements")
	}
}
