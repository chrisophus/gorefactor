# GoRefactor Repo Check - Quick Reference

## Quick Start

```bash
# Analyze current directory
./gorefactor-repo-check -dir .

# Analyze specific directory
./gorefactor-repo-check -dir ./pkg

# Save as text file
./gorefactor-repo-check -dir . -output report.txt

# Save as JSON file
./gorefactor-repo-check -dir . -json -output report.json

# Output to stdout as JSON
./gorefactor-repo-check -dir . -json
```

## Output Structure

### Text Report Sections

1. **SUMMARY** - Overall health score and issue counts
2. **CRITICAL ISSUES** - Must-fix items (file size, etc.)
3. **CODE SMELLS** - Design issues by type
4. **MOST AFFECTED FILES** - Which files need most work
5. **RECOMMENDED AUTOFIXES** - Commands to run immediately
6. **RECOMMENDATIONS** - Prioritized action items

### JSON Report Structure

```json
{
  "summary": {
    "total_issues": 135,
    "critical_issues": 1,
    "overall_health_score": 55.5
  },
  "critical_issues": [...],
  "file_size_issues": [...],
  "code_smells": [...],
  "smells_by_category": {...},
  "recommended_autofixes": [...],
  "issues_by_file": {...}
}
```

## Understanding the Health Score

- **80-100**: ✅ Excellent - Few issues, good code quality
- **60-80**: 🟡 Good - Some improvements recommended
- **40-60**: ⚠️ Fair - Multiple issues need attention
- **20-40**: 🔴 Poor - Significant refactoring needed
- **0-20**: 🚨 Critical - Major issues, urgent action required

## Issue Severity Levels

| Level | Icon | Impact |
|-------|------|--------|
| Critical | 🚨 | Must fix immediately |
| Error | 🔴 | High priority issues |
| Medium | 🟠 | Should address soon |
| Low | 🟡 | Consider improving |
| Info | ℹ️ | Nice to have improvements |

## Common Code Smells

### God Object 🐛
- **Problem**: Struct/type with too many fields (>10)
- **Why it matters**: Violates Single Responsibility, hard to test
- **Fix**: Break into smaller, focused types

### Large Class 📦
- **Problem**: Type with many fields and/or methods
- **Why it matters**: Too many responsibilities in one place
- **Fix**: Extract groups of related functionality

### Switch Statements 🔄
- **Problem**: Type-based switching scattered across codebase
- **Why it matters**: Missing abstraction, tight coupling to concrete types
- **Fix**: Use interfaces and polymorphism

### Excessive Parameters 📥
- **Problem**: Function with too many parameters (>5-6)
- **Why it matters**: Hard to use, hard to test, error-prone
- **Fix**: Group parameters into structs, use builders

### Data Clumps 📋
- **Problem**: Same parameter groups appear repeatedly
- **Why it matters**: Missed abstraction opportunity
- **Fix**: Extract into parameter objects

### File Size 📄
- **Problem**: Files exceeding size limits (default: 300 lines)
- **Why it matters**: Harder to understand, navigate, and test
- **Fix**: Split into multiple files using `gorefactor split`

## Workflows

### Quick Health Check
```bash
./gorefactor-repo-check -dir . | head -20
```

### Get Only Critical Issues
```bash
./gorefactor-repo-check -dir . -json | jq '.critical_issues'
```

### Check Specific Package
```bash
./gorefactor-repo-check -dir ./pkg/mypackage
```

### Generate Trend Report
```bash
# Run monthly and save with date
./gorefactor-repo-check -dir . -json -output "report-$(date +%Y-%m-%d).json"

# Compare with previous month
jq .summary previous-report.json current-report.json
```

### Filter by Smell Type
```bash
./gorefactor-repo-check -dir . -json | jq '.smells_by_category["God Object"]'
./gorefactor-repo-check -dir . -json | jq '.smells_by_category["Switch Statements"]'
```

### Get Files Most in Need of Refactoring
```bash
./gorefactor-repo-check -dir . -json | jq '.issues_by_file | to_entries | sort_by(.value | length) | reverse | .[0:10]'
```

## Integration with CI/CD

### GitHub Actions Example
```yaml
- name: Code Quality Analysis
  run: |
    ./gorefactor-repo-check -dir . -json -output report.json
    
    # Fail if health score is below threshold
    score=$(jq '.summary.overall_health_score' report.json)
    if (( $(echo "$score < 60" | bc -l) )); then
      echo "Health score too low: $score"
      exit 1
    fi
```

### Parse for Slack Notification
```bash
./gorefactor-repo-check -dir . -json | jq '
{
  score: .summary.overall_health_score,
  critical: .summary.critical_issues,
  issues: .summary.total_issues,
  files: .summary.files_affected
}' > metrics.json
```

## Comparison with Standard Tools

| Task | Tool | Command |
|------|------|---------|
| Design issues | gorefactor-repo-check | `./gorefactor-repo-check -dir .` |
| Unused variables | golangci-lint | `golangci-lint run` |
| Complex functions | gorefactor recommend | `./gorefactor recommend file.go` |
| Test coverage | go tool | `go test -cover ./...` |

## Troubleshooting

### "command not found: gorefactor"
- Ensure you've built the tool: `go build -o gorefactor ./cmd/gorefactor`
- Or ensure it's in your PATH

### Report is empty
- Check if `./gorefactor` binary exists and is executable
- Verify the directory path is correct
- Check for permission issues

### JSON parsing errors
- Ensure jq is installed: `apt-get install jq` or `brew install jq`
- Use the `-json` flag explicitly: `./gorefactor-repo-check -dir . -json`

## Files and Artifacts

| File | Purpose |
|------|---------|
| `gorefactor-repo-check` | Binary executable |
| `REPO_HEALTH_REPORT.txt` | Human-readable report |
| `REPO_HEALTH_REPORT.json` | Machine-readable report |
| `REPO_CHECK_SUMMARY.md` | Detailed analysis document |
| `REPO_CHECK_USAGE.md` | This file |

## Advanced Usage

### Custom Analysis Script
```bash
#!/bin/bash
# Run analysis and extract specific metrics

./gorefactor-repo-check -dir . -json | jq '{
  health: .summary.overall_health_score,
  critical_files: (.issues_by_file | to_entries | map(select(.value | length > 5)) | length),
  god_objects: (.smells_by_category["God Object"] | length // 0),
  switch_statements: (.smells_by_category["Switch Statements"] | length // 0)
}'
```

### Track Improvements Over Time
```bash
# Add this to your build pipeline
score=$(./gorefactor-repo-check -dir . -json | jq '.summary.overall_health_score')
echo "$(date +%Y-%m-%d),$score" >> health-history.csv

# Plot with your favorite tool
# gnuplot, matplotlib, or upload to analytics
```

## Contact & Support

For issues with the analysis tool, check:
1. The main CLAUDE.md for project conventions
2. REPO_CHECK_SUMMARY.md for detailed findings
3. Run with -json flag to see raw data for debugging
