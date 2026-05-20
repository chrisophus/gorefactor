package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

type LintIssue struct {
	File       string `json:"file"`
	Rule       string `json:"rule"`
	Severity   string `json:"severity"`
	Message    string `json:"message"`
	AutoFix    string `json:"autofix,omitempty"`
	AutoFixCmd string `json:"autofixCmd,omitempty"`
}

type LintOutput struct {
	Issues []LintIssue `json:"issues"`
}

type RepoCheckReport struct {
	Summary                  Summary            `json:"summary"`
	CriticalIssues           []LintIssue        `json:"critical_issues"`
	FileSizeIssues           []LintIssue        `json:"file_size_issues"`
	CodeSmells               []LintIssue        `json:"code_smells"`
	SmellsByCategory         map[string][]LintIssue `json:"smells_by_category"`
	RecommendedAutofixes     []AutofixRecommendation `json:"recommended_autofixes"`
	Recommendations          []Recommendation   `json:"recommendations"`
	IssuesByFile             map[string][]LintIssue `json:"issues_by_file"`
}

type AutofixRecommendation struct {
	File       string `json:"file"`
	Message    string `json:"message"`
	Command    string `json:"command"`
	Priority   string `json:"priority"`
}

type Summary struct {
	TotalIssues        int     `json:"total_issues"`
	CriticalCount      int     `json:"critical_issues"`
	ErrorCount         int     `json:"error_issues"`
	MediumCount        int     `json:"medium_issues"`
	LowCount           int     `json:"low_issues"`
	FilesAffected      int     `json:"files_affected"`
	OverallHealthScore float64 `json:"overall_health_score"`
	TopSmellCategory   string  `json:"top_smell_category"`
}

type Recommendation struct {
	Category    string `json:"category"`
	Priority    string `json:"priority"`
	Description string `json:"description"`
	Affected    int    `json:"affected_count"`
	Impact      string `json:"impact"`
}

func main() {
	dir := flag.String("dir", ".", "Directory to analyze")
	output := flag.String("output", "", "Output file (default: stdout)")
	jsonOutput := flag.Bool("json", false, "Output as JSON")
	flag.Parse()

	report := &RepoCheckReport{
		Summary:                Summary{},
		CriticalIssues:        []LintIssue{},
		FileSizeIssues:        []LintIssue{},
		CodeSmells:            []LintIssue{},
		SmellsByCategory:      make(map[string][]LintIssue),
		RecommendedAutofixes:  []AutofixRecommendation{},
		Recommendations:       []Recommendation{},
		IssuesByFile:          make(map[string][]LintIssue),
	}

	// Run gorefactor lint
	runLintAnalysis(*dir, report)

	// Generate recommendations
	generateRecommendations(report)
	calculateHealthScore(report)

	// Output
	if *jsonOutput {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error marshaling JSON: %v\n", err)
			os.Exit(1)
		}
		if *output != "" {
			err := os.WriteFile(*output, data, 0644)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error writing file: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("JSON report written to %s\n", *output)
		} else {
			fmt.Println(string(data))
		}
	} else {
		printReport(report, *output)
	}
}

func runLintAnalysis(dir string, report *RepoCheckReport) {
	cmd := exec.Command("./gorefactor", "lint", dir, "--json")
	output, err := cmd.Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: lint command failed: %v\n", err)
		return
	}

	var lintOutput LintOutput
	if err := json.Unmarshal(output, &lintOutput); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to parse lint output: %v\n", err)
		return
	}

	filesAffected := make(map[string]bool)

	for _, issue := range lintOutput.Issues {
		filesAffected[issue.File] = true

		// Categorize by severity and type
		switch issue.Severity {
		case "error":
			report.CriticalIssues = append(report.CriticalIssues, issue)
			if issue.Rule == "file-size" {
				report.FileSizeIssues = append(report.FileSizeIssues, issue)
				if issue.AutoFixCmd != "" {
					report.RecommendedAutofixes = append(report.RecommendedAutofixes, AutofixRecommendation{
						File:     issue.File,
						Message:  issue.Message,
						Command:  issue.AutoFixCmd,
						Priority: "high",
					})
				}
			}
		case "medium", "low":
			if issue.Rule == "smell" {
				report.CodeSmells = append(report.CodeSmells, issue)
				// Categorize smell by type
				smellType := extractSmellType(issue.Message)
				report.SmellsByCategory[smellType] = append(report.SmellsByCategory[smellType], issue)
			}
		}

		report.IssuesByFile[issue.File] = append(report.IssuesByFile[issue.File], issue)
	}

	// Update summary
	report.Summary.TotalIssues = len(lintOutput.Issues)
	report.Summary.FilesAffected = len(filesAffected)
	report.Summary.CriticalCount = len(report.CriticalIssues)

	for _, issue := range lintOutput.Issues {
		switch issue.Severity {
		case "error":
			report.Summary.ErrorCount++
		case "medium":
			report.Summary.MediumCount++
		case "low":
			report.Summary.LowCount++
		}
	}

	// Find top smell category
	maxCount := 0
	topCategory := ""
	for category, issues := range report.SmellsByCategory {
		if len(issues) > maxCount {
			maxCount = len(issues)
			topCategory = category
		}
	}
	report.Summary.TopSmellCategory = topCategory
}

func extractSmellType(message string) string {
	// Extract the primary smell type from the message
	if strings.Contains(message, "God Object") {
		return "God Object"
	}
	if strings.Contains(message, "Large Class") {
		return "Large Class"
	}
	if strings.Contains(message, "Switch Statements") {
		return "Switch Statements"
	}
	if strings.Contains(message, "Excessive Parameters") {
		return "Excessive Parameters"
	}
	if strings.Contains(message, "Duplicate") {
		return "Code Duplication"
	}
	if strings.Contains(message, "Unused") {
		return "Unused Code"
	}
	if strings.Contains(message, "Circular") {
		return "Circular Dependency"
	}
	return "Other"
}

func generateRecommendations(report *RepoCheckReport) {
	if report.Summary.CriticalCount > 0 {
		report.Recommendations = append(report.Recommendations, Recommendation{
			Category:    "File Size",
			Priority:    "critical",
			Description: "Files exceed size limits and should be split",
			Affected:    len(report.FileSizeIssues),
			Impact:      "Large files are harder to test, understand, and maintain",
		})
	}

	if len(report.CodeSmells) > 0 {
		report.Recommendations = append(report.Recommendations, Recommendation{
			Category:    "Code Quality",
			Priority:    "high",
			Description: fmt.Sprintf("Multiple code smells detected (primary: %s)", report.Summary.TopSmellCategory),
			Affected:    len(report.CodeSmells),
			Impact:      "Code smells indicate design issues that lead to bugs and complexity",
		})
	}

	// Specific recommendations by smell type
	if godObjects := report.SmellsByCategory["God Object"]; len(godObjects) > 0 {
		report.Recommendations = append(report.Recommendations, Recommendation{
			Category:    "Design",
			Priority:    "high",
			Description: "God Objects: Structs with too many fields should be broken into smaller types",
			Affected:    len(godObjects),
			Impact:      "Violates Single Responsibility Principle, hard to test and extend",
		})
	}

	if switches := report.SmellsByCategory["Switch Statements"]; len(switches) > 0 {
		report.Recommendations = append(report.Recommendations, Recommendation{
			Category:    "Design",
			Priority:    "medium",
			Description: "Switch Statements: Type-based switching pattern scattered across codebase",
			Affected:    len(switches),
			Impact:      "Indicates missing abstraction or polymorphism opportunity",
		})
	}

	if excessive := report.SmellsByCategory["Excessive Parameters"]; len(excessive) > 0 {
		report.Recommendations = append(report.Recommendations, Recommendation{
			Category:    "Code Quality",
			Priority:    "medium",
			Description: "Functions with excessive parameters should be refactored",
			Affected:    len(excessive),
			Impact:      "Makes functions harder to use and test",
		})
	}

	if len(report.RecommendedAutofixes) > 0 {
		report.Recommendations = append(report.Recommendations, Recommendation{
			Category:    "Automation",
			Priority:    "high",
			Description: fmt.Sprintf("%d file(s) can be auto-fixed using gorefactor split", len(report.RecommendedAutofixes)),
			Affected:    len(report.RecommendedAutofixes),
			Impact:      "Autofix can immediately improve code structure",
		})
	}
}

func calculateHealthScore(report *RepoCheckReport) {
	score := 100.0

	// Deductions based on issue counts and severity
	score -= float64(report.Summary.ErrorCount) * 20.0
	score -= float64(report.Summary.MediumCount) * 3.0
	score -= float64(report.Summary.LowCount) * 0.5

	if score < 0 {
		score = 0
	}

	report.Summary.OverallHealthScore = score
}

func printReport(report *RepoCheckReport, output string) {
	var w *os.File
	var err error
	if output != "" {
		w, err = os.Create(output)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating file: %v\n", err)
			os.Exit(1)
		}
		defer w.Close()
	} else {
		w = os.Stdout
	}

	fmt.Fprintf(w, "╔════════════════════════════════════════════════════════════════╗\n")
	fmt.Fprintf(w, "║          GOREFACTOR REPOSITORY CODE QUALITY REPORT             ║\n")
	fmt.Fprintf(w, "╚════════════════════════════════════════════════════════════════╝\n\n")

	// Summary
	fmt.Fprintf(w, "📊 SUMMARY\n")
	fmt.Fprintf(w, "─────────────────────────────────────────────────────────────────\n")
	healthEmoji := "✅"
	if report.Summary.OverallHealthScore < 80 {
		healthEmoji = "⚠️ "
	}
	if report.Summary.OverallHealthScore < 50 {
		healthEmoji = "🚨"
	}
	fmt.Fprintf(w, "%s Overall Health Score: %.1f/100\n", healthEmoji, report.Summary.OverallHealthScore)
	fmt.Fprintf(w, "📈 Total Issues: %d\n", report.Summary.TotalIssues)
	fmt.Fprintf(w, "🔴 Critical (Error): %d | 🟠 Medium: %d | 🟡 Low: %d\n",
		report.Summary.ErrorCount, report.Summary.MediumCount, report.Summary.LowCount)
	fmt.Fprintf(w, "📁 Files Affected: %d\n", report.Summary.FilesAffected)
	if report.Summary.TopSmellCategory != "" {
		fmt.Fprintf(w, "🐛 Top Issue Type: %s\n", report.Summary.TopSmellCategory)
	}
	fmt.Fprintf(w, "\n")

	// Critical Issues
	if len(report.CriticalIssues) > 0 {
		fmt.Fprintf(w, "🚨 CRITICAL ISSUES (%d)\n", len(report.CriticalIssues))
		fmt.Fprintf(w, "─────────────────────────────────────────────────────────────────\n")
		for _, issue := range report.CriticalIssues {
			fmt.Fprintf(w, "  📄 %s\n", issue.File)
			fmt.Fprintf(w, "     %s\n", issue.Message)
			if issue.AutoFixCmd != "" {
				fmt.Fprintf(w, "     🔧 Autofix: %s\n", issue.AutoFixCmd)
			}
		}
		fmt.Fprintf(w, "\n")
	}

	// Smell Categories Summary
	if len(report.SmellsByCategory) > 0 {
		fmt.Fprintf(w, "🐛 CODE SMELLS BY TYPE (%d total)\n", len(report.CodeSmells))
		fmt.Fprintf(w, "─────────────────────────────────────────────────────────────────\n")

		// Sort categories by count
		type categoryCount struct {
			name  string
			count int
		}
		var sorted []categoryCount
		for name, issues := range report.SmellsByCategory {
			sorted = append(sorted, categoryCount{name, len(issues)})
		}
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].count > sorted[j].count
		})

		for _, cc := range sorted {
			fmt.Fprintf(w, "  • %s (%d occurrences)\n", cc.name, cc.count)
			// Show a few examples
			count := 0
			for _, issue := range report.SmellsByCategory[cc.name] {
				if count >= 2 {
					fmt.Fprintf(w, "    ... and %d more\n", len(report.SmellsByCategory[cc.name])-count)
					break
				}
				fmt.Fprintf(w, "    - %s: %s\n", issue.File, truncateMessage(issue.Message, 60))
				count++
			}
		}
		fmt.Fprintf(w, "\n")
	}

	// Most affected files
	if len(report.IssuesByFile) > 0 {
		fmt.Fprintf(w, "📋 MOST AFFECTED FILES\n")
		fmt.Fprintf(w, "─────────────────────────────────────────────────────────────────\n")

		type fileCount struct {
			file  string
			count int
		}
		var fileCounts []fileCount
		for file, issues := range report.IssuesByFile {
			fileCounts = append(fileCounts, fileCount{file, len(issues)})
		}
		sort.Slice(fileCounts, func(i, j int) bool {
			return fileCounts[i].count > fileCounts[j].count
		})

		for i, fc := range fileCounts {
			if i >= 10 {
				fmt.Fprintf(w, "  ... and %d more files\n", len(fileCounts)-i)
				break
			}
			fmt.Fprintf(w, "  %d. %s (%d issues)\n", i+1, fc.file, fc.count)
		}
		fmt.Fprintf(w, "\n")
	}

	// Recommended Autofixes
	if len(report.RecommendedAutofixes) > 0 {
		fmt.Fprintf(w, "🔧 RECOMMENDED AUTOFIXES (%d)\n", len(report.RecommendedAutofixes))
		fmt.Fprintf(w, "─────────────────────────────────────────────────────────────────\n")
		for i, fix := range report.RecommendedAutofixes {
			if i >= 5 {
				fmt.Fprintf(w, "  ... and %d more\n", len(report.RecommendedAutofixes)-i)
				break
			}
			fmt.Fprintf(w, "  📄 %s\n", fix.File)
			fmt.Fprintf(w, "     %s\n", fix.Message)
			fmt.Fprintf(w, "     $ %s\n", fix.Command)
		}
		fmt.Fprintf(w, "\n")
	}

	// Recommendations
	if len(report.Recommendations) > 0 {
		fmt.Fprintf(w, "💡 RECOMMENDATIONS\n")
		fmt.Fprintf(w, "─────────────────────────────────────────────────────────────────\n")
		for _, rec := range report.Recommendations {
			icon := "ℹ️ "
			if rec.Priority == "critical" {
				icon = "🚨"
			} else if rec.Priority == "high" {
				icon = "🔴"
			} else if rec.Priority == "medium" {
				icon = "🟠"
			}
			fmt.Fprintf(w, "  %s [%s] %s\n", icon, rec.Category, rec.Description)
			fmt.Fprintf(w, "     Affected: %d | Impact: %s\n", rec.Affected, rec.Impact)
		}
		fmt.Fprintf(w, "\n")
	}

	fmt.Fprintf(w, "═════════════════════════════════════════════════════════════════\n\n")

	if report.Summary.TotalIssues == 0 {
		fmt.Fprintf(w, "✅ No issues detected! Repository is in good shape.\n\n")
	}
}

func truncateMessage(msg string, maxLen int) string {
	if len(msg) <= maxLen {
		return msg
	}
	return msg[:maxLen] + "..."
}
