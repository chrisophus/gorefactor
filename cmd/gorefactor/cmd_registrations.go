package main

// Registrations for the analysis (read-only) commands whose implementations
// predate the self-registration registry. Mutation commands register
// themselves from their own files; new commands should do the same — add a
// file with an init() that calls registerCommand, no central edits needed.

func init() {
	extractBlockL9()
	registerCommand(Command{
		Name:        "analyze-diff",
		Description: "Analyze a diff file and generate a refactoring plan",
		Usage:       "analyze-diff <diff.patch> [output-plan.json]",
		MinArgs:     1,
		MaxArgs:     2,
		Run:         analyzeDiff,
	})
	extractBlockL41()
	extractBlockL50()
	extractBlockL59()
	extractBlockL85()
	extractBlockL94()
	extractBlockL103()
	registerCommand(Command{
		Name:        "find-package-deps",
		Description: "Analyze package dependencies and detect circular imports [--json]",
		Usage:       "find-package-deps <dir> [--json]",
		MinArgs:     1,
		MaxArgs:     1,
		Flags:       map[string]bool{"--json": false},
		Run:         findPackageDepsCommand,
	})
	registerCommand(Command{
		Name:        "inspect",
		Description: "One-page summary of a Go file: decls, sizes, lint issues, extraction candidates",
		Usage:       "inspect <file.go> [--max N]",
		MinArgs:     1,
		MaxArgs:     1,
		Flags:       map[string]bool{"--max": true},
		Run:         inspectCommand,
	})
	registerCommand(Command{
		Name:        "init-agent-rules",
		Description: "Write the gorefactor agent-rules snippet into CLAUDE.md / .cursorrules / AGENTS.md [--target ...]; with --mcp also emit a .mcp.json pointing a client at `gorefactor mcp`",
		Usage:       "init-agent-rules [--target claude.md|cursor|agents.md|all] [--mcp] [--mcp-only]",
		MinArgs:     0,
		MaxArgs:     0,
		Flags:       map[string]bool{"--target": true, "--mcp": false, "--mcp-only": false},
		Run:         initAgentRulesCommand,
	})
	registerCommand(Command{
		Name:        "suggest-plan",
		Description: "Suggest refactoring opportunities for a file [--output plan.json] [--json] [--patterns]",
		Usage:       "suggest-plan <file.go> [--output plan.json] [--json] [--patterns]",
		MinArgs:     1,
		MaxArgs:     1,
		Flags:       map[string]bool{"--output": true, "--json": false, "--patterns": false},
		Run:         suggestPlanCommand,
	})
	registerCommand(Command{
		Name:        "repl",
		Description: "Interactive REPL mode for step-by-step refactoring",
		Usage:       "repl",
		MinArgs:     0,
		MaxArgs:     0,
		Run:         replCommand,
	})
	registerCommand(Command{
		Name:        "architect",
		Description: "Generate a starter go-arch-lint.yml from the current import graph [--suggest] [--output path] [dir]",
		Usage:       "architect --suggest [--output path] [dir]",
		MinArgs:     0,
		MaxArgs:     1,
		Flags:       map[string]bool{"--suggest": false, "--output": true, "-o": true},
		Run:         architectCommand,
	})
}

func extractBlockL9() {
	registerCommand(Command{
		Name:        "recommend",
		Description: "Recommend code blocks for method extraction",
		Usage:       "recommend <file.go> [<Func>] [--short] [--reduce-complexity [--threshold N] [--apply] [--json]] [--function NAME] [--min-complexity N] [--max-complexity N] [--min-statements N] [--max-statements N] [--max-read-vars N] [--max-write-vars N] [--num-leading-stmts N]",
		MinArgs:     0,
		MaxArgs:     2,
		Flags: map[string]bool{
			"--help":              false,
			"--short":             false,
			"--reduce-complexity": false,
			"--apply":             false,
			"--threshold":         true,
			"--json":              false,
			"--function":          true,
			"--min-complexity":    true,
			"--max-complexity":    true,
			"--max-read-vars":     true,
			"--max-write-vars":    true,
			"--min-statements":    true,
			"--max-statements":    true,
			"--num-leading-stmts": true,
		},
		Run: recommendExtractions,
	})
}

func extractBlockL41() {
	registerCommand(Command{
		Name:        "analyze-file-sizes",
		Description: "Analyze Go files in a directory for size issues and extraction opportunities",
		Usage:       "analyze-file-sizes <directory> [--max-size N] [--format json|text]",
		MinArgs:     1,
		MaxArgs:     1,
		Flags:       map[string]bool{"--max-size": true, "--format": true},
		Run:         analyzeFileSizes,
	})
}

func extractBlockL50() {
	registerCommand(Command{
		Name:        "exec",
		Description: "Execute a single operation from inline JSON or stdin (supports piping)",
		Usage:       "exec [json|-]",
		MinArgs:     0,
		MaxArgs:     1,
		Flags:       map[string]bool{"-stdin": false},
		Run:         execOperation,
	})
}

func extractBlockL59() {
	registerCommand(Command{
		Name:        "lint",
		Description: "Run structural lints (file size, duplicates) [--fix [--verify]] [--json] [--max N] [--fail-only]",
		Usage:       "lint [path] [--fix] [--verify] [--fix-level safe|aggressive] [--baseline] [--write-baseline] [--baseline-file PATH] [--json] [--quiet] [--fail-only] [--info] [--verbose] [--max N] [--rule NAME] [--skip-rule NAME] [--fail-on error|warning] [--config PATH] [--profile NAME]",
		MinArgs:     0,
		MaxArgs:     1,
		Flags: map[string]bool{
			"--baseline":       false,
			"--write-baseline": false,
			"--baseline-file":  true,
			"--fix":            false,
			"--verify":         false,
			"--fix-level":      true,
			"--json":           false,
			"--quiet":          false,
			"--fail-only":      false,
			"--info":           false,
			"--verbose":        false,
			"--cpuprofile":     true,
			"--profile-rules":  false,
			"--config":         true,
			"--profile":        true,
			"--max":            true,
			"--rule":           true,
			"--skip-rule":      true,
			"--fail-on":        true,
		},
		Run: lintCommand,
	})
}

func extractBlockL85() {
	registerCommand(Command{
		Name:        "find-callers",
		Description: "Find all callers of a function or method [--in path] [--json]",
		Usage:       "find-callers <Func|Receiver:Method> [--in path] [--json]",
		MinArgs:     1,
		MaxArgs:     1,
		Flags:       map[string]bool{"--in": true, "--json": false},
		Run:         findCallersCommand,
	})
}

func extractBlockL94() {
	registerCommand(Command{
		Name:        "find-uses",
		Description: "Find all uses of a symbol [--in path] [--json]",
		Usage:       "find-uses <Symbol|Receiver:Method> [--in path] [--json]",
		MinArgs:     1,
		MaxArgs:     1,
		Flags:       map[string]bool{"--in": true, "--json": false},
		Run:         findUsesCommand,
	})
}

func extractBlockL103() {
	registerCommand(Command{
		Name:        "find-implementations",
		Description: "Find all types that implement an interface [--in path] [--json]",
		Usage:       "find-implementations <Interface> [--in path] [--json]",
		MinArgs:     1,
		MaxArgs:     1,
		Flags:       map[string]bool{"--in": true, "--json": false},
		Run:         findImplementationsCommand,
	})
}
