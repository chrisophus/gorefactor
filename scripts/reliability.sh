#!/usr/bin/env bash
# Phase 3 reliability battery for the second-tier (cheap-LLM) agent.
#
# Runs the default agentic mode N times over a controlled task set on
# THIS repo, resetting to the runtime HEAD between every run (safe:
# captured at start, never a hardcoded SHA). Aggregates RUN_METRICS +
# exit codes into RELIABILITY.md: success / punt / error rates, mean
# steps, local tokens (frontier tokens are 0 by construction).
#
# NOTE: commit any uncommitted work before running — the per-run
# `git clean -fd` will delete untracked files in the repo.
#
# Usage: scripts/reliability.sh [iters] [model] [api-base]
set -u
ITERS="${1:-3}"
MODEL="${2:-qwen2.5-coder:14b}"
APIBASE="${3:-http://localhost:11434/v1}"
REPO="$(cd "$(dirname "$0")/.." && pwd)"
GRA="${GRA:-/tmp/gra}"
BASE="$(git -C "$REPO" rev-parse HEAD)"
RES="$(mktemp)"
echo "battery: iters=$ITERS model=$MODEL base=$(git -C "$REPO" rev-parse --short HEAD)"

run() {            # $1=label  $2=spec
  for i in $(seq 1 "$ITERS"); do
    git -C "$REPO" reset --hard "$BASE" -q && git -C "$REPO" clean -fdq
    out="$("$GRA" -dir "$REPO" -provider openai -api-base "$APIBASE" \
            -model "$MODEL" -max-iter 12 -spec "$2" 2>&1)"
    ec=$?
    m="$(printf '%s\n' "$out" | sed -n 's/.*<<<RUN_METRICS \(.*\) RUN_METRICS>>>.*/\1/p' | tail -1)"
    [ -z "$m" ] && m='{"outcome":"error","steps":0,"local_tokens":0}'
    printf '{"label":"%s","exit":%d,"m":%s}\n' "$1" "$ec" "$m" >> "$RES"
    echo "  $1 #$i -> exit=$ec $m"
  done
}

run scaffold  'Create cmd/gorefactor-agent/relmeta.go in package main with exactly: func RelMeta() string { return "ok" }'
run rename    'Rename the unexported function camelToSnake to camelToSnakeCase in package cmd/gorefactor and update all references.'
run movefunc  'Move the top-level function camelToSnake to a new file cmd/gorefactor/case_convert.go in the same package. Do not change anything else.'
run analysis  'List the files and line numbers of every caller of the function emitRunMetrics. Do not modify any code; report the answer.'
run infeasible 'Rewrite the duplicate-block detection in the analyzer package to use a rolling hash for linear-time performance.'

git -C "$REPO" reset --hard "$BASE" -q && git -C "$REPO" clean -fdq
echo "restored to $(git -C "$REPO" rev-parse --short HEAD); aggregating..."

python3 - "$RES" "$REPO/RELIABILITY.md" "$MODEL" "$ITERS" <<'PY'
import json, sys, collections, datetime
res, outpath, model, iters = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4]
rows=[json.loads(l) for l in open(res) if l.strip()]
agg=collections.defaultdict(lambda: dict(n=0,fixed=0,punt=0,error=0,steps=0,toks=0))
for r in rows:
    a=agg[r["label"]]; m=r["m"]; a["n"]+=1
    o=m.get("outcome","error"); a[o if o in ("fixed","punt","error") else "error"]+=1
    a["steps"]+=m.get("steps",0); a["toks"]+=m.get("local_tokens",0)
def pct(x,n): return f"{(100*x/n):.0f}%" if n else "-"
lines=[]
lines.append("# Reliability battery — second-tier agent\n")
lines.append(f"_model `{model}`, {iters} run(s)/task, gate = go build+test, "
             f"resets to runtime HEAD between runs, generated {datetime.date.today()}_\n")
lines.append("| task class | runs | success | punt | error | mean steps | local tokens | frontier tokens |")
lines.append("|---|--:|--:|--:|--:|--:|--:|--:|")
tot=dict(n=0,fixed=0,punt=0,error=0,toks=0)
for label in ("scaffold","rename","movefunc","analysis","infeasible"):
    a=agg.get(label)
    if not a: continue
    n=a["n"]
    lines.append(f"| {label} | {n} | {pct(a['fixed'],n)} | {pct(a['punt'],n)} "
                 f"| {pct(a['error'],n)} | {a['steps']/n:.1f} | {a['toks']} | 0 |")
    for k in ("n","fixed","punt","error","toks"): tot[k]+=a[k]
n=tot["n"]
lines.append(f"| **all** | {n} | {pct(tot['fixed'],n)} | {pct(tot['punt'],n)} "
             f"| {pct(tot['error'],n)} | - | {tot['toks']} | **0** |")
lines.append("\n## Reading this\n")
lines.append("- **success** = task done AND `go build`+`go test` green (gate is ground "
             "truth); for `analysis` it is a `report` answer (no gate — nothing changed).\n"
             "- **punt** = junior cleanly handed back (warm report, repo restored) — a *correct* "
             "outcome for `infeasible`, a miss for `scaffold`/`rename`/`movefunc`/`analysis`.\n"
             "- **error** = infrastructure failure (should be ~0).\n"
             "- **frontier tokens = 0**: every run is entirely local; each success is frontier "
             "spend avoided, each punt costs the senior only a warm report.\n")
open(outpath,"w").write("\n".join(lines)+"\n")
print("wrote", outpath)
PY
rm -f "$RES"
