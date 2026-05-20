# GoRefactor Repo Check - Configuration Guide

## Overview

The `gorefactor-repo-check` tool can be customized using a YAML configuration file. This allows you to:

- **Enable/disable specific checks**
- **Configure severity thresholds** for each check
- **Customize health score calculation**
- **Set CI/CD integration thresholds**
- **Exclude files and packages** from analysis
- **Fine-tune report output**

## Quick Start

```bash
# Use default config (.gorefactor-check.yaml)
./gorefactor-repo-check -dir .

# Use custom config file
./gorefactor-repo-check -dir . -config /path/to/custom-config.yaml

# Display loaded configuration
./gorefactor-repo-check -show-config
```

## Configuration File Location

The tool looks for configuration in this order:

1. **File specified with `-config` flag**
2. **`.gorefactor-check.yaml` in current directory** (default)
3. **Fallback to hardcoded defaults** if config file not found

## Configuration Structure

### Global Settings

```yaml
version: "1.0"
name: "My Custom Config"
description: "Optional description of this config"

analysis:
  enabled: true
  exclude:
    - vendor/
    - testdata/
    - "*_test.go"
```

### Check Configuration

Each check can be individually configured:

```yaml
checks:
  file_size:
    enabled: true                    # Enable/disable this check
    max_lines: 300                   # Threshold
    severity: "error"                # error | medium | low | info
    description: "Optional description"
    autofix: "command template"      # Pattern with {placeholder}
    autofix_enabled: false           # Can suggest autofix
```

## Available Checks

### 1. File Size Check

Detects files that exceed the line count limit.

```yaml
checks:
  file_size:
    enabled: true
    max_lines: 300
    severity: "error"
    autofix: "gorefactor split <file> --max {max_lines}"
```

**Thresholds**:
- 300+ lines: Increasingly hard to test and maintain
- 500+ lines: Consider as critical
- 1000+ lines: Definitely critical

### 2. God Object Check

Detects structs with too many fields.

```yaml
checks:
  god_object:
    enabled: true
    max_fields: 10
    severity: "error"
```

**Impact**: Violates Single Responsibility Principle

**Example**:
```go
type BlockInfo struct {
    Name string        // 16 fields total = God Object
    File string
    // ... 14 more fields
}
```

### 3. Large Class Check

Detects types with excessive members.

```yaml
checks:
  large_class:
    enabled: true
    max_members: 15
    severity: "medium"
```

**Members include**: fields + methods

### 4. Switch Statements Check

Detects type-based switching patterns scattered across codebase.

```yaml
checks:
  switch_statements:
    enabled: true
    severity: "medium"
    pattern_detection: true
```

**Impact**: Indicates missing abstraction or polymorphism opportunity

### 5. Excessive Parameters Check

Detects functions with too many parameters.

```yaml
checks:
  excessive_parameters:
    enabled: true
    max_parameters: 6
    severity: "medium"
```

**Example**:
```go
func ProcessData(a, b, c, d, e, f, g int) {  // 7 params = excessive
    // ...
}
```

### 6. Data Clumps Check

Detects parameter groups that appear together repeatedly.

```yaml
checks:
  data_clumps:
    enabled: true
    min_occurrences: 3  # Only flag if appears 3+ times
    severity: "low"
```

**Example**:
```go
// These three appear together in multiple functions
funcA(file string, line int, col int)
funcB(file string, line int, col int)  // Pattern detected!
funcC(file string, line int, col int)
```

### 7. Circular Dependencies Check

Detects circular imports between packages.

```yaml
checks:
  circular_dependencies:
    enabled: true
    severity: "error"
```

### 8. Untested Packages Check

Detects packages without test coverage.

```yaml
checks:
  untested_packages:
    enabled: true
    severity: "medium"
```

### 9. Duplication Check

Detects duplicate code blocks.

```yaml
checks:
  duplication:
    enabled: true
    min_block_lines: 10  # Only flag blocks larger than this
    severity: "low"
```

## Health Score Configuration

Customize how the health score is calculated:

```yaml
health_score:
  # Points deducted per issue
  error_weight: 20.0      # Critical issues
  medium_weight: 3.0      # Medium severity
  low_weight: 0.5         # Low severity

  # Score thresholds
  threshold_critical: 40   # Score below this = critical alert
  threshold_warning: 60    # Score below this = warning
  threshold_good: 80       # Score above this = excellent

  # Maximum possible score
  max_score: 100.0
```

### Health Score Interpretation

| Score Range | Status | Interpretation |
|-------------|--------|-----------------|
| 80-100 | ✅ Excellent | Few issues, high code quality |
| 60-80 | 🟡 Good | Some improvements recommended |
| 40-60 | ⚠️ Fair | Multiple issues need attention |
| 20-40 | 🔴 Poor | Significant refactoring needed |
| 0-20 | 🚨 Critical | Urgent action required |

## Reporting Configuration

Control report output:

```yaml
reporting:
  include_file_details: true
  include_recommendations: true
  include_affected_files: true
  max_files_to_show: 10
  max_issues_per_file: 5

  # Formatting
  show_severity_icons: true
  show_autofix_commands: true
  truncate_long_messages: true
  max_message_length: 80
```

## Filtering Configuration

```yaml
filtering:
  min_severity: "low"    # Only show issues at this level or higher
  group_by: "category"   # category | file | severity
  sort_by: "severity"    # severity | file | count
```

## CI/CD Integration

Configure behavior for continuous integration:

```yaml
ci:
  # Fail pipeline if score drops below this
  fail_threshold: 50

  # Warn (don't fail) if score below this
  warn_threshold: 70

  # Exit codes
  exit_codes:
    success: 0    # Successful analysis
    warning: 0    # Warnings detected (don't fail)
    error: 1      # Errors detected (fail)
```

### Example GitHub Actions Integration

```yaml
- name: Code Quality Analysis
  run: |
    ./gorefactor-repo-check -dir . -json -output report.json
    exit_code=$?
    
    score=$(jq '.summary.overall_health_score' report.json)
    echo "Health Score: $score"
    
    exit $exit_code
```

## Configuration Examples

### Strict Configuration (Enterprise)

```yaml
version: "1.0"
name: "Enterprise Code Quality Standards"

checks:
  file_size:
    enabled: true
    max_lines: 200      # Stricter limit
    severity: "error"
    
  god_object:
    enabled: true
    max_fields: 8       # Stricter limit
    severity: "error"
    
  excessive_parameters:
    enabled: true
    max_parameters: 5   # Stricter limit
    severity: "error"

health_score:
  error_weight: 25.0
  medium_weight: 5.0
  threshold_critical: 50
  threshold_warning: 70

ci:
  fail_threshold: 75    # High bar for CI
```

### Lenient Configuration (Legacy Project)

```yaml
version: "1.0"
name: "Legacy Project Standards"

checks:
  file_size:
    enabled: true
    max_lines: 500      # More lenient
    severity: "medium"  # Less critical
    
  god_object:
    enabled: true
    max_fields: 15      # More lenient
    severity: "medium"
    
  data_clumps:
    enabled: false      # Disabled for legacy code
    
  duplication:
    enabled: false      # Too much existing duplication

health_score:
  error_weight: 10.0    # Less harsh penalties
  medium_weight: 1.0
  threshold_critical: 20
  threshold_warning: 40

ci:
  fail_threshold: 30    # Low bar for legacy
```

### Minimal Configuration (Getting Started)

```yaml
version: "1.0"

checks:
  file_size:
    enabled: true
    max_lines: 300
    severity: "error"
    
  god_object:
    enabled: true
    max_fields: 10
    severity: "medium"
    
  # Disable everything else for initial focus
  large_class:
    enabled: false
  switch_statements:
    enabled: false
  excessive_parameters:
    enabled: false
  data_clumps:
    enabled: false
  circular_dependencies:
    enabled: false
  untested_packages:
    enabled: false
  duplication:
    enabled: false
```

## Configuration by Use Case

### New Project (High Standards)

Focus on preventing issues from the start:
- Low thresholds
- All checks enabled
- Strict CI gates

### Existing Project (Gradual Improvement)

Focus on preventing new issues:
- Higher thresholds
- Baseline checks only
- Gradual threshold reduction

### Open Source (Community Standards)

Balance maintainability with contributor ease:
- Medium thresholds
- Focus on design issues
- Clear recommendations

## Configuration Management

### Version Control

Commit `.gorefactor-check.yaml` to your repository:

```bash
git add .gorefactor-check.yaml
git commit -m "Add code quality configuration"
```

### Team Consistency

Keep the config in sync across your team:

```bash
# Share the config
cp .gorefactor-check.yaml ~/team-standards/

# Use in CI/CD
./gorefactor-repo-check -dir . -config ~/team-standards/.gorefactor-check.yaml
```

### Environment-Specific Configs

Use different configs for different environments:

```bash
# Development (lenient)
./gorefactor-repo-check -dir . -config config/dev.yaml

# Staging (medium)
./gorefactor-repo-check -dir . -config config/staging.yaml

# Production (strict)
./gorefactor-repo-check -dir . -config config/prod.yaml
```

## Troubleshooting

### Config File Not Found

```bash
# Specify explicit path
./gorefactor-repo-check -dir . -config /absolute/path/config.yaml

# Or ensure it's in current directory
ls -la .gorefactor-check.yaml
```

### YAML Parse Errors

```bash
# Validate YAML syntax
yamllint .gorefactor-check.yaml

# Or use online validators
```

### Config Not Applied

```bash
# Display loaded config
./gorefactor-repo-check -show-config

# Verify thresholds match your config
```

## Best Practices

1. **Start with defaults** - Use the default config initially
2. **Gradually tighten** - Lower thresholds as code improves
3. **Team agreement** - Discuss standards with your team
4. **Version control** - Commit config to track changes
5. **Document changes** - Note why thresholds changed
6. **Monitor trends** - Track score changes over time
7. **Automate** - Integrate into CI/CD pipeline

## Advanced: Custom Thresholds

Each project may have different needs. Examples:

```yaml
# Machine Learning project (complex algorithms allowed)
checks:
  excessive_parameters:
    max_parameters: 10  # Algorithms need more params
  file_size:
    max_lines: 500      # Complex implementations get longer

# High-frequency trading (strict reliability)
checks:
  god_object:
    max_fields: 5       # Simplicity for reliability
  file_size:
    max_lines: 200      # Easier to review and audit
```

## See Also

- `REPO_CHECK_USAGE.md` - Tool usage guide
- `REPO_CHECK_SUMMARY.md` - Detailed analysis examples
- `.gorefactor-check.yaml` - Default configuration
