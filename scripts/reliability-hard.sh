#!/usr/bin/env bash
# Reliability battery — HARDER classes, designed to discriminate between
# the local qwen junior and an API model like Haiku.
#
# The base reliability.sh set saturated both at 80% success / 20% punt,
# so it cannot answer "where is Haiku better than local?". This script
# probes three different stress modes — multi-step coordination, symbol
# disambiguation, cross-file mechanical change — each plausibly harder
# than anything in the base battery.
#
# Same invocation surface as scripts/reliability.sh:
#   scripts/reliability-hard.sh [iters] [model] [api-base] [provider] [outfile]
set -u
ITERS="${1:-2}"
MODEL="${2:-qwen2.5-coder:14b}"
APIBASE="${3-http://localhost:11434/v1}"
PROVIDER="${4:-openai}"
REPO="$(cd "$(dirname "$0")/.." && pwd)"
OUT="${5:-$REPO/RELIABILITY-HARD.md}"
GRA="${GRA:-/tmp/gra}"
BASE="$(git -C "$REPO" rev-parse HEAD)"
RES="$(mktemp)"
echo "battery-hard: iters=$ITERS provider=$PROVIDER model=$MODEL base=$(git -C "$REPO" rev-parse --short HEAD) out=$OUT"

run() {            # $1=label  $2=spec
  for i in $(seq 1 "$ITERS"); do
    git -C "$REPO" reset --hard "$BASE" -q && git -C "$REPO" clean -fdq
    rm -rf "$REPO/.gorefactor"
    t0=$(date +%s)
    if [ -n "$APIBASE" ]; then
      out="$("$GRA" -dir "$REPO" -provider "$PROVIDER" -api-base "$APIBASE" \
              -model "$MODEL" -max-iter 12 -spec "$2" 2>&1)"
    else
      out="$("$GRA" -dir "$REPO" -provider "$PROVIDER" \
              -model "$MODEL" -max-iter 12 -spec "$2" 2>&1)"
    fi
    ec=$?
    secs=$(( $(date +%s) - t0 ))
    m="$(printf '%s\n' "$out" | sed -n 's/.*<<<RUN_METRICS \(.*\) RUN_METRICS>>>.*/\1/p' | tail -1)"
    [ -z "$m" ] && m='{"outcome":"error","steps":0,"local_tokens":0}'
    printf '{"label":"%s","exit":%d,"secs":%d,"m":%s}\n' "$1" "$ec" "$secs" "$m" >> "$RES"
    echo "  $1 #$i -> exit=$ec ${secs}s $m"
  done
}

# multistep: two coordinated ops in one spec (move + rename + callers).
# Tests whether the junior sequences dependent operations correctly.
run multistep   'Move the top-level function camelToSnake to a new file cmd/gorefactor/case_convert.go in the same package, then rename it to camelToSnakeCase and update all callers. Both changes must land before the gate.'

# disambig: rename a method on one receiver only (Tokens exists on
# *anthropicProvider AND *openAIProvider; only the anthropic one is
# being renamed). Tests receiver-scoped symbol disambiguation.
run disambig    'Rename the method Tokens on the receiver *anthropicProvider to TokenUsage. Do NOT rename the Tokens method on *openAIProvider — leave it untouched.'

# multifile: add a leading parameter to an unexported function and
# update every caller. There is no single gorefactor op for "add
# parameter + update callers"; the junior must read each call site
# and edit it. ~7 callers across 4 files.
run multifile   'Add ctx context.Context as the first parameter of the unexported function runIn in cmd/gorefactor-agent/loop.go, and update every caller in the package to pass context.Background() as the new first argument. The function body does not need to use ctx; just thread it through the signature and call sites.'

git -C "$REPO" reset --hard "$BASE" -q && git -C "$REPO" clean -fdq
rm -rf "$REPO/.gorefactor"
echo "restored to $(git -C "$REPO" rev-parse --short HEAD); aggregating..."

python3 - "$RES" "$OUT" "$MODEL" "$ITERS" "$PROVIDER" <<'PY'
import json, sys, collections, datetime
res, outpath, model, iters = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4]
provider = sys.argv[5] if len(sys.argv) > 5 else "openai"
rows=[json.loads(l) for l in open(res) if l.strip()]
agg=collections.defaultdict(lambda: dict(n=0,fixed=0,punt=0,error=0,steps=0,toks=0,secs=0))
for r in rows:
    a=agg[r["label"]]; m=r["m"]; a["n"]+=1
    o=m.get("outcome","error"); a[o if o in ("fixed","punt","error") else "error"]+=1
    a["steps"]+=m.get("steps",0); a["toks"]+=m.get("local_tokens",0)
    a["secs"]+=r.get("secs",0)
def pct(x,n): return f"{(100*x/n):.0f}%" if n else "-"
lines=[]
lines.append("# Reliability battery — HARDER classes\n")
lines.append(f"_provider `{provider}`, model `{model}`, {iters} run(s)/task, "
             f"gate = go build+test, resets to runtime HEAD between runs, "
             f"generated {datetime.date.today()}_\n")
lines.append("| task class | runs | success | punt | error | mean steps | mean secs | local tokens | frontier tokens |")
lines.append("|---|--:|--:|--:|--:|--:|--:|--:|--:|")
tot=dict(n=0,fixed=0,punt=0,error=0,toks=0,secs=0)
for label in ("multistep","disambig","multifile"):
    a=agg.get(label)
    if not a: continue
    n=a["n"]
    lines.append(f"| {label} | {n} | {pct(a['fixed'],n)} | {pct(a['punt'],n)} "
                 f"| {pct(a['error'],n)} | {a['steps']/n:.1f} | {a['secs']/n:.0f} | {a['toks']} | 0 |")
    for k in ("n","fixed","punt","error","toks","secs"): tot[k]+=a[k]
n=tot["n"]
lines.append(f"| **all** | {n} | {pct(tot['fixed'],n)} | {pct(tot['punt'],n)} "
             f"| {pct(tot['error'],n)} | - | {tot['secs']/n:.0f} | {tot['toks']} | **0** |")
open(outpath,"w").write("\n".join(lines)+"\n")
print("wrote", outpath)
PY
rm -f "$RES"
