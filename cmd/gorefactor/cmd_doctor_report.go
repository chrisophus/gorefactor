package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/chrisophus/gorefactor/analyzer"
	"github.com/chrisophus/gorefactor/doctor"
)

// structuralSubstrate adapts the in-process structural linter (the 28 rules)
// to the doctor engine — kept as a first-class substrate per plan decision 8.
// It lives in package main because the rules do; the agent loop composes the
// library substrates without it (structural findings are warning-severity and
// never gate).
type structuralSubstrate struct {
	configPath string
}

// Info implements doctor.Substrate. Gating is false: struct-category findings
// are warning severity by design (plan decision 3b).
func (structuralSubstrate) Info() doctor.SubstrateInfo {
	return doctor.SubstrateInfo{Name: "structural", ScopeCapable: true}
}

// Run implements doctor.Substrate.
func (s structuralSubstrate) Run(ctx doctor.RunContext) ([]doctor.Finding, error) {
	opts := lintOptions{root: ctx.Root, configPath: s.configPath, failOn: "error"}
	if err := opts.loadConfig(); err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	walk := analyzer.DefaultWalkOptions()
	if opts.cfg != nil {
		walk = opts.cfg.WalkOptions()
	}
	files, err := structuralScopeFiles(ctx, walk)
	if err != nil {
		return nil, fmt.Errorf("structural scope files: %w", err)
	}
	issues := runLintRules(defaultLintRules(), opts.lintContext(files), opts)
	issues = applyConfigSeverity(issues, opts)
	findings := make([]doctor.Finding, 0, len(issues))
	for _, iss := range issues {
		sev := doctor.SeverityWarning
		if iss.Severity == "info" {
			sev = doctor.SeverityInfo
		}
		file, line := splitLintFilePos(iss.File)
		findings = append(findings, doctor.Finding{
			File:     file,
			Line:     line,
			Rule:     iss.Rule,
			Category: doctor.CategoryStruct,
			Severity: sev,
			Message:  iss.Message,
			FixCmd:   iss.AutoFixCmd,
		})
	}
	return findings, nil
}

// structuralScopeFiles lists the .go files to lint: the scope dirs when the
// run is scoped, the whole tree otherwise.
func structuralScopeFiles(ctx doctor.RunContext, walk analyzer.WalkOptions) ([]string, error) {
	if len(ctx.ScopeDirs) == 0 {
		return collectGoFiles(ctx.Root, walk)
	}
	var files []string
	for _, dir := range ctx.ScopeDirs {
		matches, err := filepath.Glob(filepath.Join(ctx.Root, dir, "*.go"))
		if err != nil {
			return nil, fmt.Errorf("glob: %w", err)
		}
		for _, m := range matches {
			if !analyzer.ShouldSkipFile(m, walk) {
				files = append(files, m)
			}
		}
	}
	return files, nil
}

func splitLintFilePos(file string) (string, int) {
	rest := file
	line := 0
	for i := 0; i < 2; i++ {
		idx := strings.LastIndex(rest, ":")
		if idx < 0 {
			break
		}
		n, err := strconv.Atoi(rest[idx+1:])
		if err != nil {
			break
		}
		rest, line = rest[:idx], n
	}
	if !strings.HasSuffix(rest, ".go") {
		return file, 0
	}
	return rest, line
}

// doctorSubstrates composes the CLI's substrate set.
func doctorSubstrates(configPath string) []doctor.Substrate {
	return []doctor.Substrate{
		structuralSubstrate{configPath: configPath},
		doctor.Golangci{},
		doctor.APIDiff{},
	}
}

// doctorReportCommand implements `doctor --report [--base ref] [--scoped]`:
// the merged diagnose Report, advisory-first (plan decision 7) — it prints new
// findings and substrate availability but always exits zero so it can never
// block a commit while rules are still earning trust. --scoped matches the
// agent gate's per-edit behavior: only the packages touched vs the base ref
// (plus depth-1 reverse deps) are analyzed; full-run-only substrates are
// skipped with a recorded status and FixedCount is omitted.
func doctorReportCommand(opts doctorOpts) error {
	rep, err := doctor.Diagnose(doctor.Options{
		Root:       opts.root,
		BaseRef:    opts.baseRef,
		Substrates: doctorSubstrates(opts.configPath),
		ConfigPath: opts.configPath,
		Scoped:     opts.scoped,
	})

	if err != nil {
		return fmt.Errorf("diagnose: %w", err)
	}
	if opts.jsonOut {
		return json.NewEncoder(os.Stdout).Encode(rep)
	}
	printDoctorReport(rep)
	return nil
}

func printDoctorReport(rep *doctor.Report) {
	fmt.Printf("doctor report (base %s)\n", rep.BaseRef)
	if len(rep.Scope) > 0 {
		fmt.Printf("  scope: %s\n", strings.Join(rep.Scope, ", "))
	}

	for _, s := range rep.Substrates {
		mark := "ok"
		if s.State != doctor.SubstrateRan {
			mark = string(s.State)
			if s.Detail != "" {
				mark += ": " + s.Detail
			}
		}
		fmt.Printf("  [%-10s] %s\n", s.Name, mark)
	}
	newFindings := 0
	for _, f := range rep.Findings {
		if !f.New {
			continue
		}
		newFindings++
		if f.Severity == doctor.SeverityInfo {
			continue // counted below; info is advisory noise in the human view
		}
		loc := f.File
		if f.Line > 0 {
			loc = fmt.Sprintf("%s:%d", f.File, f.Line)
		}
		fix := ""
		if f.FixCmd != "" {
			fix = " (fix: " + f.FixCmd + ")"
		}
		fmt.Printf("  [%s/%s] %s: %s%s\n", f.Severity, f.Category, loc, f.Message, fix)
	}
	fmt.Printf("  new: %d error, %d warning, %d info; pre-existing suppressed: %d\n",
		rep.NewCount[doctor.SeverityError], rep.NewCount[doctor.SeverityWarning],
		rep.NewCount[doctor.SeverityInfo], len(rep.Findings)-newFindings)
	if len(rep.NewErrors()) > 0 {
		fmt.Println("  advisory: new error-severity findings above would fail the gate once it goes hard")
	}
}
