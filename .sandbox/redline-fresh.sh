#!/usr/bin/env bash
# Sandbox Studio's review gate (ticket_ready.fresh in .sandbox/sandbox.yaml): prints
# HEAD_FRESH=true when redline-pr-watch (ansible-k8s, workstation/redline-pr-watch) has posted a
# review of the pull request's current head commit, HEAD_FRESH=false otherwise. The studio runs it
# in its own clone with GH_TOKEN and GH_REPO set: usage redline-fresh.sh <pr>.
set -euo pipefail
pr="${1:?usage: redline-fresh.sh <pr>}"
head="$(gh api "repos/$GH_REPO/pulls/$pr" --jq .head.sha)"
if gh api --paginate "repos/$GH_REPO/pulls/$pr/reviews" --jq '.[].body' |
  grep -qE "<!-- redline-review:v1 verdict=[a-z]+ head=$head -->"; then
  echo HEAD_FRESH=true
else
  echo HEAD_FRESH=false
fi
