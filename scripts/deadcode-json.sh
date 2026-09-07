#!/bin/sh
# scripts/deadcode-json.sh — flatten `deadcode -json` (array of packages, each
# with a Funcs list) into the flat array of findings Redline's json format maps.
# One object per unreachable function: file, line, and a message naming it.
exec deadcode -json ./... 2>/dev/null | python3 -c '
import json, sys
try:
    pkgs = json.load(sys.stdin)
except Exception:
    pkgs = []
out = []
for p in pkgs or []:
    for f in p.get("Funcs") or []:
        pos = f.get("Position") or {}
        out.append({
            "file": pos.get("File", ""),
            "line": pos.get("Line", 0),
            "rule": "unreachable",
            "message": "unreachable func %s.%s" % (p.get("Name", ""), f.get("Name", "")),
            "severity": "warning",
        })
json.dump(out, sys.stdout)
'
