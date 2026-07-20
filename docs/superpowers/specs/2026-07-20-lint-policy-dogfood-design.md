# Focused lint policy dogfooding

## Goal

Make GoRefactor useful as a clean, enforceable lint layer in Marketplace Core without
introducing a generalized policy language. Marketplace Core should gate differentiated,
high-confidence rules, retain a small actionable advisory surface, and avoid duplicating
golangci-lint findings.

## GoRefactor changes

### Tracked artifact allowances

Add an optional `tracked_artifact` configuration block:

```yaml
tracked_artifact:
  allow_extensions:
    - .png
    - .ico
    - .tgz
  allow_path_prefixes:
    - docs/assets/
    - ui/public/
    - ui/vendor/
```

Paths are repository-relative and slash-normalized. Extensions are case-insensitive and
normalized to include a leading dot. A tracked file is exempt when either an allowed extension
or path prefix matches. Existing behavior remains unchanged when the block is absent.

### Focused rule tuning

Add only the configuration needed by the current consumer:

```yaml
lint:
  exclude_test_files:
    - error-not-wrapped
  exclude_packages:
    high-coupling:
      - internal/domain
      - internal/wire
  thresholds:
    high-coupling:
      fan_in: 12
      fan_out: 15
```

Rule names are validated against the registry. Package paths are module-relative. These settings
apply before severity remapping and baseline comparison, so excluded findings do not enter the
baseline.

This is intentionally not a generalized rule-policy DSL. Record a backlog item in the project
review for future per-rule path globs, file classes, thresholds, and rule-specific options once
multiple consumers demonstrate a common shape.

### Baseline configuration

Allow the committed baseline to be enabled from YAML:

```yaml
baseline:
  enabled: true
  file: .gorefactor-lint-baseline.json
```

CLI baseline flags override configuration; add `--no-baseline` to disable a configured baseline
for an exploratory run. Relative baseline paths resolve from the lint root. Writing a baseline
remains an explicit CLI operation; configuration never rewrites it.

### Tier contract

Document:

- `error`: deterministic CI gate.
- `warning`: actionable debt, normally held by a committed baseline ratchet.
- `info`: opt-in exploration shown with `--info`.

`--quiet --fail-only` is documented as intentionally silent when no finding reaches `--fail-on`.

## Marketplace Core changes

Reduce `.gorefactor.yaml` to:

- Errors for differentiated logging/error-flow, sentinel, and file-size rules.
- Warnings for actionable harness/config hygiene such as stranded comments,
  orphaned config paths, advertised-but-unwired behavior, god objects, and tracked artifacts.
- Info for exploratory test/extraction sensors.
- Off for duplicate golangci-lint coverage and low-confidence structural smells.

Re-enable `tracked-artifact` with explicit allowances for documentation images, the favicon, and
vendored design-system archives. Enable the baseline ratchet for retained warnings.

Keep `lint-gorefactor` as the quiet CI gate, but run it at `--fail-on warning`; the configured
baseline makes only new or worsened warnings visible and failing. Add
`lint-gorefactor-advisory` for visible warning and info output. The target names and comments must
explain this distinction.

Remove the three stale `.golangci.yml` path exemptions reported by `orphaned-config-path`. Remove
or reattach the eight comments reported by `stranded-comment`, after checking each declaration so
documentation is not lost.

## Verification

GoRefactor uses test-first development:

- Config parsing and normalization tests.
- Tracked artifact allow-extension and allow-prefix tests.
- Rule exclusion and coupling-threshold tests.
- Baseline config precedence and path-resolution tests.
- Existing defaults remain covered for backward compatibility.

Run focused Go tests, `go test ./...`, `make build`, and `make gate`.

Marketplace Core verification:

- Run the pinned GoRefactor version that contains these features.
- Generate and commit the baseline after the cleanup.
- Confirm `make lint-gorefactor` is silent and passing.
- Confirm the advisory target emits only retained, intentional findings.
- Run repository-required build, scoped tests, `make lint-gorefactor`, and the normal pre-commit
  gate.

## Delivery

GoRefactor lands and releases first. Marketplace Core then pins that release and adopts the new
configuration. The repositories use separate feature branches and reviewable commits.
