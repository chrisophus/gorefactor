# YAML Configuration Support - Feature Summary

## What Was Added

I've added full **YAML configuration support** to `gorefactor-repo-check`, allowing you to customize every aspect of code quality analysis without modifying the source code.

## Key Features

### 1. Enable/Disable Checks Individually
```yaml
checks:
  file_size:
    enabled: true     # Turn on/off
    max_lines: 300    # Configure threshold
    severity: error   # Set severity level
```

### 2. Customize Health Score Calculation
```yaml
health_score:
  error_weight: 20.0      # Points deducted per critical issue
  medium_weight: 3.0      # Points deducted per medium issue
  threshold_critical: 40  # Score below this = critical alert
```

### 3. CI/CD Integration Thresholds
```yaml
ci:
  fail_threshold: 50   # Fail pipeline if score drops below this
  warn_threshold: 70   # Warn if score below this
  exit_codes:
    success: 0
    error: 1
```

### 4. Control Report Output
```yaml
reporting:
  include_file_details: true
  show_autofix_commands: true
  max_files_to_show: 10
  truncate_long_messages: true
```

## Usage

```bash
# Use default configuration (.gorefactor-check.yaml)
./gorefactor-repo-check -dir .

# Use custom configuration
./gorefactor-repo-check -dir . -config /path/to/config.yaml

# Display loaded configuration
./gorefactor-repo-check -show-config

# Show configuration before running
./gorefactor-repo-check -config config-examples/strict.yaml -show-config
```

## Example Configurations

### 1. Default (Balanced)
- **File**: `.gorefactor-check.yaml`
- **Use case**: Most Go projects
- **Health Score**: Reasonable thresholds
- **Status**: Included by default

### 2. Strict (Enterprise)
- **File**: `config-examples/strict.yaml`
- **Use case**: Enterprise projects, high-quality standards
- **Health Score**: 0/100 (shows what's wrong)
- **Thresholds**: Very strict (200 lines max, 8 fields max)
- **CI Gate**: Fail if score < 75

### 3. Lenient (Legacy)
- **File**: `config-examples/lenient.yaml`
- **Use case**: Existing/legacy codebases
- **Health Score**: Gradual improvement path
- **Thresholds**: Relaxed (500 lines, 20 fields)
- **CI Gate**: Fail if score < 20 (low bar)

### 4. Minimal (Starting Out)
- **File**: `config-examples/minimal.yaml`
- **Use case**: Initial setup, focus on critical issues
- **Checks**: Only critical items enabled
- **Reports**: High severity issues only
- **CI Gate**: Fail if score < 50

### 5. Design-Focused
- **File**: `config-examples/focus-design.yaml`
- **Use case**: Architecture reviews, pattern improvements
- **Focus**: Switch statements, god objects, data clumps
- **Reports**: Full design pattern descriptions
- **CI Gate**: Fail if score < 60

## Configuration Comparison

| Aspect | Strict | Default | Lenient | Minimal |
|--------|--------|---------|---------|---------|
| Max File Size | 200 | 300 | 500 | 400 |
| Max Fields (Struct) | 8 | 10 | 20 | 15 |
| Max Parameters | 4 | 6 | 8 | 7 |
| Data Clumps Check | Yes | Yes | No | No |
| Duplication Check | Yes | Yes | No | No |
| Error Weight | 25.0 | 20.0 | 5.0 | 20.0 |
| CI Fail Threshold | 75 | 50 | 20 | 50 |

## Real-World Examples

### Example 1: Enforce Standards (Strict Config)
```bash
# Team wants maximum code quality
./gorefactor-repo-check -dir . -config config-examples/strict.yaml

# Result: 0/100 (lots to fix)
# CI Gate: Fail until score reaches 75+
```

### Example 2: Gradual Improvement (Lenient → Stricter)
```bash
# Month 1: Start with lenient config
./gorefactor-repo-check -dir . -config config-examples/lenient.yaml
# Score: 35/100

# Month 2: Improve and tighten slightly
./gorefactor-repo-check -dir . -config config-examples/minimal.yaml
# Score: 45/100

# Month 3: Use default config
./gorefactor-repo-check -dir .
# Score: 55/100

# Month 4: Aim for strict compliance
./gorefactor-repo-check -dir . -config config-examples/strict.yaml
# Score: 75/100+ (goal achieved!)
```

### Example 3: CI/CD Integration
```bash
# In GitHub Actions
- name: Code Quality Gate
  run: |
    # Use project's standard config
    ./gorefactor-repo-check -dir . \
      -config config-examples/strict.yaml \
      -json \
      -output report.json
    
    # Tool exits with configured code
    # CI will fail if exit code != 0
```

## File Locations

```
repository/
├── .gorefactor-check.yaml          # Default config (included)
├── CONFIG_GUIDE.md                 # Complete configuration guide
├── CONFIG_FEATURE_SUMMARY.md       # This file
├── config-examples/
│   ├── strict.yaml                 # Enterprise standards
│   ├── lenient.yaml                # Legacy project standards
│   ├── minimal.yaml                # Getting started
│   └── focus-design.yaml           # Design pattern focus
└── cmd/gorefactor-repo-check/
    └── main.go                     # Tool implementation
```

## How to Choose a Configuration

| Your Situation | Recommended Config |
|---|---|
| New project, high standards | `strict.yaml` |
| Existing project, gradual improvement | `lenient.yaml` |
| Just getting started | `minimal.yaml` |
| Architecture/design review | `focus-design.yaml` |
| Most Go projects (default) | `.gorefactor-check.yaml` |
| Custom needs | Create your own |

## Creating Custom Configurations

1. **Start with a template** from `config-examples/`
2. **Adjust thresholds** based on your team's standards
3. **Enable/disable checks** relevant to your project
4. **Set health score weights** reflecting your priorities
5. **Configure CI gates** for your pipeline
6. **Test it**: `./gorefactor-repo-check -show-config`

## Advanced: Team Configurations

Create multiple configs for different purposes:

```bash
# Shared team standards
team-standards/
├── production.yaml      # High bar for production code
├── development.yaml     # Lenient for WIP branches
├── review.yaml          # For code review gates
└── onboarding.yaml      # For new projects
```

Use in CI/CD:
```bash
./gorefactor-repo-check -config team-standards/production.yaml
```

## Documentation

See **CONFIG_GUIDE.md** for:
- Complete configuration reference
- All available checks and thresholds
- Health score calculation details
- CI/CD integration examples
- Best practices
- Troubleshooting

## What This Enables

✅ **Flexibility** - Different projects, different standards
✅ **Team Alignment** - Share config files for consistency
✅ **Gradual Improvement** - Tighten thresholds incrementally
✅ **CI/CD Integration** - Custom exit codes and gates
✅ **Reproducible Builds** - Same config = same results
✅ **Version Control** - Track config changes over time
✅ **Experimentation** - Try different thresholds easily

## Next Steps

1. **Understand your baseline**: Run with default config
2. **Choose a config**: Pick one that matches your needs
3. **Test it locally**: `./gorefactor-repo-check -config config.yaml`
4. **Integrate with CI/CD**: Add to your pipeline
5. **Track improvements**: Run monthly and watch the score rise
6. **Adjust as needed**: Tighten thresholds as code quality improves
