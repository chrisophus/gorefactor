#!/usr/bin/env python3
"""
Phase 2, step 1: collect SWE-agent trajectories for fine-tuning.

Two source modes:
  --from-lint   Auto-generate specs from `gorefactor lint` findings (default).
                Runs gorefactor lint, converts each fixable finding to a spec,
                runs SWE-agent for each, and saves the trajectory.
  --specs FILE  Read specs from a plain-text file (one spec per line).

Usage:
  # Auto-generate from lint (recommended for bootstrapping):
  python sweagent/finetune/collect_traces.py --from-lint --repo . --output sweagent/finetune/traces

  # From a hand-written specs file:
  python sweagent/finetune/collect_traces.py --specs sweagent/finetune/specs.txt --repo . --output sweagent/finetune/traces

  # Dry-run: print specs without running SWE-agent:
  python sweagent/finetune/collect_traces.py --from-lint --repo . --dry-run
"""

import argparse
import json
import os
import subprocess
import sys
import time
from pathlib import Path


# ── Spec generation from lint findings ───────────────────────────────────────

# Rules that have clear single-operation fixes expressible via gorefactor tools.
# Maps lint rule → template for a gorefactor-style spec.
FIXABLE_RULES = {
    "error-not-wrapped": (
        "Wrap bare 'return err' statements in function {symbol} in {file} "
        "using fmt.Errorf so the error includes context. "
        "Use the wrap_errors tool."
    ),
    "extract-candidate": (
        "Extract the complex block in function {symbol} in {file} "
        "into a well-named helper function. "
        "Use skeleton and read_excerpt to find the exact lines, "
        "then extract_method."
    ),
    "file-size": (
        "Split {file} into smaller sibling files. "
        "Group methods by receiver type and functions by shared prefix. "
        "Use split_file."
    ),
    "long-function": (
        "Break down function {symbol} in {file} — it is too long. "
        "Extract the most self-contained block into a helper. "
        "Use skeleton, read_excerpt, then extract_method."
    ),
    "duplicate-block": (
        "The duplicate code block flagged in {file} near {symbol} "
        "should be extracted into a shared helper function. "
        "Use extract_method to pull it out, then update the duplicate call site."
    ),
}


def lint_findings(repo: Path) -> list[dict]:
    """Run gorefactor lint and return the list of issues."""
    gorefactor = repo / "gorefactor"
    if not gorefactor.exists():
        gorefactor = "gorefactor"  # fall back to PATH
    result = subprocess.run(
        [str(gorefactor), "lint", ".", "--json"],
        cwd=repo,
        capture_output=True,
        text=True,
    )
    if result.returncode != 0 and not result.stdout.strip():
        print(f"ERROR: gorefactor lint failed: {result.stderr[:200]}", file=sys.stderr)
        return []
    try:
        data = json.loads(result.stdout)
        return data.get("issues", [])
    except json.JSONDecodeError:
        print("ERROR: could not parse lint output as JSON", file=sys.stderr)
        return []


def spec_from_finding(finding: dict) -> str | None:
    """Convert a lint finding to a gorefactor spec, or None if not fixable."""
    rule = finding.get("rule", "")
    template = FIXABLE_RULES.get(rule)
    if not template:
        return None
    file_path = finding.get("file", "unknown.go")
    symbol = finding.get("symbol") or finding.get("message", "").split()[0] or "the function"
    return template.format(file=file_path, symbol=symbol)


def specs_from_lint(repo: Path) -> list[str]:
    """Generate specs from all fixable lint findings in the repo."""
    findings = lint_findings(repo)
    specs = []
    seen: set[str] = set()
    for f in findings:
        spec = spec_from_finding(f)
        if spec and spec not in seen:
            seen.add(spec)
            specs.append(spec)
    return specs


def specs_from_file(path: Path) -> list[str]:
    """Read specs from a plain-text file (one per line, # comments ignored)."""
    lines = path.read_text().splitlines()
    return [l.strip() for l in lines if l.strip() and not l.startswith("#")]


# ── SWE-agent runner ─────────────────────────────────────────────────────────

def run_sweagent(
    spec: str,
    repo: Path,
    output_dir: Path,
    instance_id: str,
    config: str = "sweagent/config.yaml",
    timeout: int = 600,
) -> dict:
    """
    Run SWE-agent for one spec. Returns a result dict:
      {"id": str, "status": "success"|"fail"|"error", "traj_path": str|None}
    """
    traj_dir = output_dir / instance_id
    traj_dir.mkdir(parents=True, exist_ok=True)

    cmd = [
        "sweagent", "run",
        "--config", config,
        "--problem_statement", spec,
        "--repo_path", str(repo),
        "--output_dir", str(traj_dir),
    ]

    try:
        result = subprocess.run(
            cmd,
            cwd=repo,
            capture_output=True,
            text=True,
            timeout=timeout,
        )
    except subprocess.TimeoutExpired:
        return {"id": instance_id, "status": "error", "reason": "timeout", "traj_path": None}
    except FileNotFoundError:
        print("ERROR: 'sweagent' not found. Install with: pip install swe-agent", file=sys.stderr)
        sys.exit(1)

    # Find the .traj file written by SWE-agent (name varies by version)
    traj_files = list(traj_dir.glob("*.traj")) + list(traj_dir.glob("*.json"))
    traj_path = str(traj_files[0]) if traj_files else None

    # Determine success: look for our <<<TASK_DONE>>> marker in stdout
    combined = result.stdout + result.stderr
    status = "success" if "<<<TASK_DONE>>>" in combined else "fail"

    return {
        "id": instance_id,
        "status": status,
        "exit_code": result.returncode,
        "traj_path": traj_path,
        "stdout_snippet": result.stdout[-500:] if result.stdout else "",
    }


# ── Main ─────────────────────────────────────────────────────────────────────

def main() -> None:
    parser = argparse.ArgumentParser(description="Collect SWE-agent trajectories for fine-tuning")
    parser.add_argument("--repo", default=".", help="Path to the Go repo (default: .)")
    parser.add_argument("--output", default="sweagent/finetune/traces", help="Output directory for trajectories")
    parser.add_argument("--config", default="sweagent/config.yaml", help="SWE-agent config file")
    parser.add_argument("--from-lint", action="store_true", help="Generate specs from gorefactor lint")
    parser.add_argument("--specs", help="Path to specs file (one spec per line)")
    parser.add_argument("--max", type=int, default=0, help="Max specs to run (0 = all)")
    parser.add_argument("--timeout", type=int, default=600, help="Per-spec timeout in seconds")
    parser.add_argument("--dry-run", action="store_true", help="Print specs without running SWE-agent")
    args = parser.parse_args()

    repo = Path(args.repo).resolve()
    output_dir = Path(args.output)
    output_dir.mkdir(parents=True, exist_ok=True)

    # Build gorefactor if needed
    gr = repo / "gorefactor"
    if not gr.exists():
        print("Building gorefactor...", flush=True)
        subprocess.run(["go", "build", "-o", "gorefactor", "./cmd/gorefactor"], cwd=repo, check=True)

    # Collect specs
    if args.specs:
        specs = specs_from_file(Path(args.specs))
        print(f"Loaded {len(specs)} specs from {args.specs}")
    elif args.from_lint:
        print("Generating specs from gorefactor lint...", flush=True)
        specs = specs_from_lint(repo)
        print(f"Generated {len(specs)} fixable specs from lint findings")
    else:
        parser.error("Supply --from-lint or --specs FILE")

    if args.max > 0:
        specs = specs[:args.max]

    if args.dry_run:
        print(f"\n{'─'*60}")
        print(f"DRY RUN — {len(specs)} specs (not running SWE-agent):")
        for i, s in enumerate(specs, 1):
            print(f"\n[{i:03d}] {s[:120]}")
        return

    # Run SWE-agent for each spec
    results: list[dict] = []
    success = fail = error = 0
    for i, spec in enumerate(specs, 1):
        instance_id = f"spec_{i:04d}"
        print(f"\n[{i}/{len(specs)}] {instance_id}: {spec[:80]}...", flush=True)
        t0 = time.time()
        r = run_sweagent(spec, repo, output_dir, instance_id, args.config, args.timeout)
        elapsed = time.time() - t0
        r["spec"] = spec
        r["elapsed_s"] = round(elapsed, 1)
        results.append(r)

        if r["status"] == "success":
            success += 1
            print(f"  ✓ success ({elapsed:.0f}s)")
        elif r["status"] == "fail":
            fail += 1
            print(f"  ✗ fail ({elapsed:.0f}s): {r.get('stdout_snippet','')[-120:]}")
        else:
            error += 1
            print(f"  ! error: {r.get('reason','')}")

    # Write results index
    index_path = output_dir / "index.json"
    index_path.write_text(json.dumps(results, indent=2))

    print(f"\n{'═'*60}")
    print(f"Done. {len(specs)} specs → {success} success, {fail} fail, {error} error")
    print(f"Results index: {index_path}")
    print(f"\nNext step:")
    print(f"  python sweagent/finetune/filter_traces.py --input {output_dir} --output sweagent/finetune/traces-ok")


if __name__ == "__main__":
    main()
