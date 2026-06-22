#!/usr/bin/env python3
"""
Phase 2, step 2: filter collected trajectories, keeping only successful ones.

A trajectory is "successful" when:
  1. The SWE-agent run ended with <<<TASK_DONE>>> in stdout (gate green), AND
  2. The trajectory file is parseable and has at least one tool call turn.
  3. Optionally: the trajectory is shorter than --max-turns (cheap models
     should solve tasks in fewer steps; long trajectories may reflect
     confused behaviour that we don't want to train on).

Usage:
  python sweagent/finetune/filter_traces.py \
    --input  sweagent/finetune/traces \
    --output sweagent/finetune/traces-ok

  # Strict: only keep short runs (fast models should finish in ≤8 tool calls)
  python sweagent/finetune/filter_traces.py \
    --input  sweagent/finetune/traces \
    --output sweagent/finetune/traces-ok \
    --max-turns 8
"""

import argparse
import json
import shutil
from pathlib import Path


SUCCESS_MARKER = "<<<TASK_DONE>>>"


def load_trajectory(traj_path: Path) -> dict | None:
    """Load a trajectory file; return None if unparseable."""
    try:
        return json.loads(traj_path.read_text())
    except (json.JSONDecodeError, OSError):
        return None


def count_tool_turns(traj: dict) -> int:
    """Count assistant turns that contain at least one tool call."""
    history = traj.get("history") or traj.get("trajectory") or []
    return sum(
        1 for msg in history
        if isinstance(msg, dict)
        and msg.get("role") == "assistant"
        and msg.get("tool_calls")
    )


def is_successful(index_entry: dict, traj: dict, max_turns: int) -> tuple[bool, str]:
    """
    Return (ok, reason). Rejects with a reason string on failure.
    """
    # Must have ended with GATE_GREEN
    if index_entry.get("status") != "success":
        return False, f"status={index_entry.get('status')}"

    # Must be parseable with at least one tool call
    n = count_tool_turns(traj)
    if n == 0:
        return False, "no tool calls found"

    # Reject long/confused trajectories
    if max_turns > 0 and n > max_turns:
        return False, f"too long ({n} turns > {max_turns})"

    return True, ""


def main() -> None:
    p = argparse.ArgumentParser(description="Filter successful SWE-agent trajectories")
    p.add_argument("--input",  required=True, help="Directory written by collect_traces.py")
    p.add_argument("--output", required=True, help="Destination for successful trajectories")
    p.add_argument("--max-turns", type=int, default=0,
                   help="Reject trajectories longer than N tool-call turns (0 = no limit)")
    args = p.parse_args()

    input_dir = Path(args.input)
    output_dir = Path(args.output)
    output_dir.mkdir(parents=True, exist_ok=True)

    index_path = input_dir / "index.json"
    if not index_path.exists():
        print(f"ERROR: {index_path} not found. Run collect_traces.py first.")
        raise SystemExit(1)

    entries: list[dict] = json.loads(index_path.read_text())

    kept: list[dict] = []
    rejected: list[dict] = []

    for entry in entries:
        traj_path_str = entry.get("traj_path")
        if not traj_path_str:
            rejected.append({**entry, "reject_reason": "no traj_path"})
            continue

        traj_path = Path(traj_path_str)
        if not traj_path.exists():
            rejected.append({**entry, "reject_reason": "traj file missing"})
            continue

        traj = load_trajectory(traj_path)
        if traj is None:
            rejected.append({**entry, "reject_reason": "unparseable"})
            continue

        ok, reason = is_successful(entry, traj, args.max_turns)
        if not ok:
            rejected.append({**entry, "reject_reason": reason})
            continue

        # Copy trajectory and spec metadata into output_dir
        dest = output_dir / entry["id"]
        dest.mkdir(exist_ok=True)
        shutil.copy2(traj_path, dest / traj_path.name)
        (dest / "meta.json").write_text(json.dumps({**entry, "traj_path": traj_path.name}, indent=2))
        kept.append(entry)

    # Write filtered index
    (output_dir / "index.json").write_text(json.dumps(kept, indent=2))
    print(f"Kept {len(kept)} / {len(entries)} trajectories → {output_dir}")
    if rejected:
        print(f"Rejected {len(rejected)}:")
        reasons: dict[str, int] = {}
        for r in rejected:
            k = r.get("reject_reason", "unknown")
            reasons[k] = reasons.get(k, 0) + 1
        for reason, count in sorted(reasons.items(), key=lambda x: -x[1]):
            print(f"  {count:3d}  {reason}")
    print(f"\nNext step:")
    print(f"  python sweagent/finetune/convert_to_sft.py --input {output_dir} --output sweagent/finetune/training_data.jsonl")


if __name__ == "__main__":
    main()
