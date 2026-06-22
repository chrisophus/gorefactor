#!/usr/bin/env python3
"""
Phase 2, step 3: convert filtered trajectories to fine-tuning JSONL.

Output format (one JSON object per line):
  {
    "messages": [
      {"role": "system",    "content": "..."},
      {"role": "user",      "content": "..."},
      {"role": "assistant", "content": "", "tool_calls": [...]},
      {"role": "tool",      "content": "...", "tool_call_id": "..."},
      ...
    ],
    "tools": [...]   # full tool schema from sweagent/tools/*.yaml
  }

This format is compatible with:
  - TRL SFTTrainer (with apply_chat_template)
  - Axolotl (function_call dataset type)
  - Most OpenAI fine-tuning endpoints

Usage:
  python sweagent/finetune/convert_to_sft.py \
    --input  sweagent/finetune/traces-ok \
    --output sweagent/finetune/training_data.jsonl

  # Split into train/val:
  python sweagent/finetune/convert_to_sft.py \
    --input  sweagent/finetune/traces-ok \
    --output sweagent/finetune/training_data.jsonl \
    --val-split 0.1
"""

import argparse
import json
import random
from pathlib import Path

import yaml


# ── Tool schema loading ───────────────────────────────────────────────────────

def load_tools(sweagent_dir: Path) -> list[dict]:
    """
    Load all tool definitions from sweagent/tools/*.yaml and return them
    in OpenAI tool-calling format (list of {"type": "function", "function": {...}}).
    """
    tools_dir = sweagent_dir / "tools"
    all_tools = []
    for yaml_path in sorted(tools_dir.glob("*.yaml")):
        data = yaml.safe_load(yaml_path.read_text())
        for t in data.get("tools", []):
            # Convert the YAML tool def to OpenAI format
            func_def: dict = {
                "name": t["name"],
                "description": t.get("description", "").strip(),
            }
            if "parameters" in t:
                func_def["parameters"] = t["parameters"]
            else:
                func_def["parameters"] = {"type": "object", "properties": {}}
            all_tools.append({"type": "function", "function": func_def})
    return all_tools


# ── Trajectory normalisation ──────────────────────────────────────────────────

def normalise_history(traj: dict) -> list[dict]:
    """
    Extract the conversation history from a trajectory, handling both
    SWE-agent v1.x ("history" key) and older formats ("trajectory" list).
    Strips any metadata-only entries; returns a flat list of chat messages.
    """
    # v1.x format
    if "history" in traj:
        return [m for m in traj["history"] if isinstance(m, dict) and "role" in m]

    # Older step-list format
    if "trajectory" in traj:
        messages = []
        for step in traj["trajectory"]:
            if not isinstance(step, dict):
                continue
            if step.get("messages"):
                messages.extend(step["messages"])
            elif step.get("response") or step.get("thought"):
                messages.append({
                    "role": "assistant",
                    "content": step.get("thought", "") + step.get("response", ""),
                })
                if step.get("observation"):
                    messages.append({
                        "role": "user",
                        "content": step["observation"],
                    })
        return messages

    return []


def truncate_tool_output(messages: list[dict], max_chars: int = 1500) -> list[dict]:
    """
    Trim long tool outputs so they don't blow the training context window.
    Keeps the first max_chars of each tool result (the part the model
    actually saw is at the start; tail is usually cut by compactMessages anyway).
    """
    out = []
    for m in messages:
        if m.get("role") == "tool" and len(m.get("content", "")) > max_chars:
            m = {**m, "content": m["content"][:max_chars] + "\n…(truncated)"}
        out.append(m)
    return out


def trajectory_to_record(traj: dict, tools: list[dict]) -> dict | None:
    """
    Convert one trajectory to a training record.
    Returns None if the trajectory is malformed or has no tool calls.
    """
    messages = normalise_history(traj)
    if not messages:
        return None

    # Must have at least one assistant turn with tool calls
    has_tool_call = any(
        m.get("role") == "assistant" and m.get("tool_calls")
        for m in messages
    )
    if not has_tool_call:
        return None

    messages = truncate_tool_output(messages)

    # Ensure the final assistant message ends with finish/report (positive signal)
    last_asst = next(
        (m for m in reversed(messages) if m.get("role") == "assistant"),
        None,
    )
    if not last_asst:
        return None
    calls = last_asst.get("tool_calls", [])
    if not calls:
        return None
    last_tool = calls[-1].get("function", {}).get("name", "")
    if last_tool not in ("finish", "report"):
        # Trajectory didn't end cleanly; skip
        return None

    return {"messages": messages, "tools": tools}


# ── Main ─────────────────────────────────────────────────────────────────────

def main() -> None:
    p = argparse.ArgumentParser(description="Convert filtered trajectories to training JSONL")
    p.add_argument("--input",  required=True, help="Directory from filter_traces.py")
    p.add_argument("--output", required=True, help="Output .jsonl path")
    p.add_argument("--sweagent-dir", default="sweagent",
                   help="Path to sweagent/ directory (for tool YAML files)")
    p.add_argument("--val-split", type=float, default=0.0,
                   help="Fraction of records to write to a separate val set (0 = no split)")
    p.add_argument("--seed", type=int, default=42, help="Random seed for val split")
    args = p.parse_args()

    input_dir  = Path(args.input)
    output_path = Path(args.output)
    sweagent_dir = Path(args.sweagent_dir)

    tools = load_tools(sweagent_dir)
    print(f"Loaded {len(tools)} tools from {sweagent_dir}/tools/")

    index = json.loads((input_dir / "index.json").read_text())
    records: list[dict] = []
    skipped = 0

    for entry in index:
        instance_id = entry["id"]
        meta_path = input_dir / instance_id / "meta.json"
        if not meta_path.exists():
            skipped += 1
            continue

        meta = json.loads(meta_path.read_text())
        traj_name = meta.get("traj_path")
        traj_file = input_dir / instance_id / traj_name if traj_name else None

        if not traj_file or not traj_file.exists():
            skipped += 1
            continue

        traj = json.loads(traj_file.read_text())
        record = trajectory_to_record(traj, tools)
        if record is None:
            skipped += 1
            continue

        records.append(record)

    print(f"Converted {len(records)} records ({skipped} skipped)")

    if not records:
        print("ERROR: no records to write. Check that filter_traces.py ran successfully.")
        raise SystemExit(1)

    random.seed(args.seed)
    random.shuffle(records)

    if args.val_split > 0:
        split = max(1, int(len(records) * args.val_split))
        val_records  = records[:split]
        train_records = records[split:]
        val_path = output_path.with_stem(output_path.stem + "_val")
        _write_jsonl(val_records, val_path)
        print(f"Val   ({len(val_records):4d} records) → {val_path}")
    else:
        train_records = records

    _write_jsonl(train_records, output_path)
    print(f"Train ({len(train_records):4d} records) → {output_path}")
    print(f"\nNext step:")
    print(f"  python sweagent/finetune/train_lora.py --data {output_path} --output ./gorefactor-qwen-7b")


def _write_jsonl(records: list[dict], path: Path) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w") as f:
        for r in records:
            f.write(json.dumps(r, ensure_ascii=False) + "\n")


if __name__ == "__main__":
    main()
