#!/usr/bin/env bash
# Dogfood: finish-first + Workspine-lite via daemon claim + real LLM.
#
# Prerequisites: make up C=api,daemon ; profile authenticated ; Claude runtime online.
#
# Usage:
#   MULTICA_PROFILE=dev-vigil-482 ./run_dogfood_finish_first.sh

set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
CLI="${MULTICA_CLI:-$ROOT/server/bin/multica}"
PROFILE="${MULTICA_PROFILE:-dev-vigil-482}"
RT="${MULTICA_RUNTIME_ID:-0c344870-3ba2-4bba-aaea-9fc0e37b261a}"
DB="${MULTICA_DATABASE:-multica_vigil_482}"
WS=$(python3 -c "import json; print(json.load(open('$HOME/.multica/profiles/$PROFILE/config.json'))['workspace_id'])")
WORKSPACES_ROOT=$(python3 -c "import json; print(json.load(open('$HOME/.multica/profiles/$PROFILE/config.json')).get('workspaces_root',''))")

mc() { "$CLI" --profile "$PROFILE" "$@"; }

curl -sf "${MULTICA_SERVER_URL:-http://127.0.0.1:18562}/health" >/dev/null
mc daemon status | head -3

AGENT_ID=$(docker exec multica-postgres-1 psql -U multica -d "$DB" -tAc \
  "SELECT id::text FROM agent WHERE workspace_id='$WS' AND name='Finish-first dogfood' AND archived_at IS NULL LIMIT 1;")
if [[ -z "$AGENT_ID" ]]; then
  INSTRUCTIONS='You are a Finish-first / Workspine dogfood agent. Read multica-finish-first. Write .multica/plans/<ISSUE-ID>.md (Goal, Steps≥2, Proofs, Status=draft). Do NOT create sub-issues, plan approve, or ask-agent. Comment the plan path and end turn.'
  mc agent create --name "Finish-first dogfood" --runtime-id "$RT" --model claude-sonnet-4-5 \
    --instructions "$INSTRUCTIONS" --visibility workspace --output json >/tmp/ff-agent.json || true
  AGENT_ID=$(docker exec multica-postgres-1 psql -U multica -d "$DB" -tAc \
    "SELECT id::text FROM agent WHERE workspace_id='$WS' AND name='Finish-first dogfood' AND archived_at IS NULL ORDER BY created_at DESC LIMIT 1;")
fi
docker exec multica-postgres-1 psql -U multica -d "$DB" -c \
  "UPDATE agent SET trust_mode='approval', effect_mode='apply' WHERE id='$AGENT_ID';" >/dev/null

STAMP=$(date +%H%M%S)
IDENT=$(mc issue create \
  --title "Dogfood finish-first workspine $STAMP" \
  --description "Finish-first dogfood only:
1. Write .multica/plans/<this-issue-id>.md (Workspine-lite).
2. Do not implement code, create sub-issues, or ask-agent.
3. Comment the plan path and stop." \
  --output json 2>/dev/null | python3 -c "import sys,re,json; print(json.loads(re.search(r'\\{[\\s\\S]*\\}', sys.stdin.read()).group())['identifier'])")
echo "issue=$IDENT agent=$AGENT_ID"
mc issue assign "$IDENT" --to-id "$AGENT_ID" >/dev/null

for i in $(seq 1 72); do
  mc issue runs "$IDENT" --output json >/tmp/ff-runs.json 2>/dev/null || true
  # primary agent completed?
  DONE=$(python3 -c "
import re
t=open('/tmp/ff-runs.json').read()
# last completed for our agent is enough when children still 0
print('yes' if '\"status\": \"completed\"' in t else 'no')
")
  echo "[$i] done_marker=$DONE"
  if [[ "$DONE" == yes ]]; then
    sleep 2
    break
  fi
  sleep 5
done

CH=$(mc issue children "$IDENT" --output json 2>/dev/null | python3 -c "import re,sys; m=re.search(r'\"total\"\\s*:\\s*(\\d+)', sys.stdin.read()); print(m.group(1) if m else '?')")
PLAN_FILE=""
if [[ -n "$WORKSPACES_ROOT" && -d "$WORKSPACES_ROOT" ]]; then
  PLAN_FILE=$(find "$WORKSPACES_ROOT" -path "*/.multica/plans/${IDENT}.md" 2>/dev/null | head -1 || true)
fi
COMMENT=$(mc issue comment list "$IDENT" --output json 2>/dev/null | python3 -c "
import re,sys
t=sys.stdin.read()
print('yes' if '.multica/plans/'+'''$IDENT''' in t or '.multica/plans/${IDENT}' in t else 'no')
")

echo "RESULT children=$CH plan_file=${PLAN_FILE:-MISSING} comment_cites_path=$COMMENT"
[[ "$CH" == 0 ]] || { echo "FAIL children"; exit 1; }
[[ -n "$PLAN_FILE" && -f "$PLAN_FILE" ]] || { echo "FAIL missing Workspine file"; exit 1; }
grep -q "## Goal\|## Steps\|## Proofs\|## Status" "$PLAN_FILE" || { echo "FAIL plan shape"; exit 1; }
[[ "$COMMENT" == yes ]] || { echo "FAIL comment did not cite plan path"; exit 1; }
echo "PASS"
