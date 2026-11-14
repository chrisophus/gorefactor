#!/usr/bin/env bash
# Sandbox Studio's review gate (ticket_ready.fresh in .sandbox/sandbox.yaml): prints
# HEAD_FRESH=true when redline-pr-watch (ansible-k8s, workstation/redline-pr-watch) has posted a
# review of the pull request's current head commit, HEAD_FRESH=false otherwise. The studio runs it
# in its own clone with GH_TOKEN and GH_REPO set: usage redline-fresh.sh <pr>.
set -euo pipefail
pr="${1:?usage: redline-fresh.sh <pr>}"
head="$(gh api "repos/$GH_REPO/pulls/$pr" --jq .head.sha)"
# Read the reviews first, so a failed call ends the script with no HEAD_FRESH line (the studio
# then says the gate could not be read) rather than reading as "not reviewed yet".
reviews="$(gh api --paginate "repos/$GH_REPO/pulls/$pr/reviews" --jq '.[].body')"
if grep -qE "<!-- redline-review:v1 verdict=[a-z]+ head=$head -->" <<<"$reviews"; then
  echo HEAD_FRESH=true
else
  echo HEAD_FRESH=false
fi
