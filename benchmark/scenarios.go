package main

func buildScenarios(root string) []scenario {
	agent := "cmd/gorefactor-agent/"
	orch := "orchestrator/"
	anl := "analyzer/"
	cmd := "cmd/gorefactor/"

	return []scenario{
		// ── Analysis (read-only) ─────────────────────────────────────────────
		{
			Name:                "find-callers: compactMessages",
			Category:            "analysis",
			ReadFiles:           glob(root, agent, "*.go"),
			GoRefactorArgs:      []string{"find-callers", "compactMessages"},
			ExpectedOutputChars: 300,
		},
		{
			Name:                "find-implementations: Provider",
			Category:            "analysis",
			ReadFiles:           glob(root, agent, "*.go"),
			GoRefactorArgs:      []string{"find-implementations", "Provider", "--in", "cmd/gorefactor-agent"},
			ExpectedOutputChars: 500,
		},
		{
			Name:                "find-uses: emitRunMetrics",
			Category:            "analysis",
			ReadFiles:           glob(root, agent, "*.go"),
			GoRefactorArgs:      []string{"find-uses", "emitRunMetrics", "--in", "cmd/gorefactor-agent"},
			ExpectedOutputChars: 400,
		},
		{
			Name:                "inspect: orchestrator/targeting.go",
			Category:            "analysis",
			ReadFiles:           []string{orch + "targeting.go"},
			GoRefactorArgs:      []string{"inspect", orch + "targeting.go"},
			ExpectedOutputChars: 800,
		},
		{
			Name:                "recommend --short: analyzer/plan_suggester.go",
			Category:            "analysis",
			ReadFiles:           []string{anl + "plan_suggester.go"},
			GoRefactorArgs:      []string{"recommend", anl + "plan_suggester.go", "--short"},
			ExpectedOutputChars: 404,
		},
		{
			Name:                "list-functions: cmd/gorefactor/main.go",
			Category:            "analysis",
			ReadFiles:           []string{cmd + "main.go"},
			GoRefactorArgs:      []string{"list-functions", cmd + "main.go"},
			ExpectedOutputChars: 600,
		},
		{
			Name:                "lint: whole repo",
			Category:            "analysis",
			ReadFiles:           glob(root, ".", "**/*.go"),
			GoRefactorArgs:      []string{"lint", "."},
			ExpectedOutputChars: 2000,
		},

		// ── Mutation (run for real, restore after) ───────────────────────────
		{
			Name:           "rename: emitRunMetrics→emitMetrics",
			Category:       "rename",
			ReadFiles:      []string{agent + "agent_tools.go"},
			WriteEstimate:  fileSize(root, agent+"agent_tools.go"),
			GoRefactorArgs: []string{"rename", agent + "agent_tools.go", "emitRunMetrics", "emitMetrics"},
			VerifyBuild:    true,
			RestoreFiles:   []string{agent + "agent_tools.go", agent + "run_agentic_driver.go", agent + "run_interactive_agentic_driver.go"},
		},
		{
			Name:           "delete --safe: senseListSymbols (has callers → refused)",
			Category:       "delete",
			ReadFiles:      []string{agent + "agent_tools.go"},
			WriteEstimate:  fileSize(root, agent+"agent_tools.go"),
			GoRefactorArgs: []string{"delete", agent + "agent_tools.go", "senseListSymbols", "--safe"},
			VerifyBuild:    false, // expected to refuse, not a build test
		},
		{
			Name:           "move: compactMessages→compact_messages.go",
			Category:       "move",
			ReadFiles:      []string{agent + "agent_tools.go"},
			WriteEstimate:  fileSize(root, agent+"agent_tools.go") + 300,
			GoRefactorArgs: []string{"move", agent + "agent_tools.go", "compactMessages", agent + "compact_messages.go"},
			VerifyBuild:    true,
			RestoreFiles:   []string{agent + "agent_tools.go"},
		},
	}
}
