#!/usr/bin/env bash
# Full dogfood: daemon claim + real LLM for multica-plan-verification.
#
# Prerequisites:
#   make up C=api   (profile matching MULTICA_PROFILE)
#   daemon running for that profile
#   server/bin/multica built
#
# Usage:
#   MULTICA_PROFILE=dev-vigil-482 ./run_dogfood_daemon.sh
#
# Env:
#   MULTICA_CLI, MULTICA_AGENT_ID (reuse), MULTICA_TRUST_MODE (default approval),
#   MULTICA_RUNTIME_ID (default Claude online runtime on dogfood workspace)

set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
CLI="${MULTICA_CLI:-$ROOT/server/bin/multica}"
PROFILE="${MULTICA_PROFILE:-dev-vigil-482}"
TRUST="${MULTICA_TRUST_MODE:-approval}"
RT="${MULTICA_RUNTIME_ID:-0c344870-3ba2-4bba-aaea-9fc0e37b261a}"
SKILL_NAME="plan-verification-dogfood"
DB="${MULTICA_DATABASE:-multica_vigil_482}"

mc() { "$CLI" --profile "$PROFILE" "$@"; }

echo "==> health"
curl -sf "${MULTICA_SERVER_URL:-http://127.0.0.1:18562}/health" >/dev/null
mc daemon status | head -5

echo "==> ensure plan_verification_gate"
TOKEN=$(python3 -c "import json; print(json.load(open('$HOME/.multica/profiles/$PROFILE/config.json'))['token'])")
WS=$(python3 -c "import json; print(json.load(open('$HOME/.multica/profiles/$PROFILE/config.json'))['workspace_id'])")
curl -sS -X PATCH "http://127.0.0.1:18562/api/workspaces/$WS" \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -H "X-Workspace-ID: $WS" \
  -d '{"settings":{"plan_verification_gate":true}}' >/dev/null

if [[ -z "${MULTICA_AGENT_ID:-}" ]]; then
  echo "==> create/find skill + agent"
  SKILL_ID=$(mc skill list --output json 2>/dev/null | python3 -c "
import json,sys,re
t=sys.stdin.read()
# tolerant: find name then nearby id is hard; use list table via psql fallback
print('')
" || true)
  SKILL_ID=$(docker exec multica-postgres-1 psql -U multica -d "$DB" -tAc \
    "SELECT id::text FROM skill WHERE workspace_id='$WS' AND name='$SKILL_NAME' LIMIT 1;")
  if [[ -z "$SKILL_ID" ]]; then
    mc skill create --name "$SKILL_NAME" \
      --description "Dogfood copy of multica-plan-verification" \
      --content-file "$ROOT/server/internal/service/builtin_skills/multica-plan-verification/SKILL.md" \
      --output json >/tmp/dogfood-skill.json || true
    SKILL_ID=$(docker exec multica-postgres-1 psql -U multica -d "$DB" -tAc \
      "SELECT id::text FROM skill WHERE workspace_id='$WS' AND name='$SKILL_NAME' LIMIT 1;")
  fi
  AGENT_ID=$(docker exec multica-postgres-1 psql -U multica -d "$DB" -tAc \
    "SELECT id::text FROM agent WHERE workspace_id='$WS' AND name='Plan verification dogfood' AND archived_at IS NULL LIMIT 1;")
  if [[ -z "$AGENT_ID" ]]; then
    INSTRUCTIONS='You are a Multica plan dogfood agent. Publish a structured plan with multica issue plan set --steps-json (2+ steps). Do NOT create sub-issues. Do NOT implement code. Do NOT call plan approve. End turn after plan set. If handoff starts with Plan verification: do not change code; multica issue plan report with findings JSON; end turn.'
    mc agent create --name "Plan verification dogfood" --runtime-id "$RT" \
      --model claude-sonnet-4-5 --instructions "$INSTRUCTIONS" --visibility workspace \
      --output json >/tmp/dogfood-agent.json || true
    AGENT_ID=$(docker exec multica-postgres-1 psql -U multica -d "$DB" -tAc \
      "SELECT id::text FROM agent WHERE workspace_id='$WS' AND name='Plan verification dogfood' AND archived_at IS NULL ORDER BY created_at DESC LIMIT 1;")
  fi
  mc agent skills add "$AGENT_ID" --skill-ids "$SKILL_ID" >/dev/null || true
else
  AGENT_ID="$MULTICA_AGENT_ID"
fi

docker exec multica-postgres-1 psql -U multica -d "$DB" -c \
  "UPDATE agent SET trust_mode='$TRUST', effect_mode='apply' WHERE id='$AGENT_ID';" >/dev/null
echo "agent=$AGENT_ID trust=$TRUST skill_bound=yes"

STAMP=$(date +%H%M%S)
IDENT=$(mc issue create \
  --title "Dogfood daemon LLM $STAMP" \
  --description "Plan only — do not implement:
1. Add a readiness probe
2. Add a probe unit test

Publish via multica issue plan set with steps-json, then STOP. Do not create sub-issues. Do not call plan approve." \
  --output json 2>/dev/null | python3 -c "import sys,re,json; print(json.loads(re.search(r'\\{[\\s\\S]*\\}', sys.stdin.read()).group())['identifier'])")
echo "issue=$IDENT"
mc issue assign "$IDENT" --to-id "$AGENT_ID" >/dev/null

echo "==> wait for publish run + verification report"
IID=$(docker exec multica-postgres-1 psql -U multica -d "$DB" -tAc \
  "SELECT id::text FROM issue WHERE workspace_id='$WS' AND number=$(echo "$IDENT" | sed 's/.*-//');")
for i in $(seq 1 72); do
  mc issue plan get "$IDENT" --output json >/tmp/dogfood-plan.json 2>/dev/null || true
  PLAN=$(python3 -c "import re; t=open('/tmp/dogfood-plan.json').read(); print('yes' if re.search(r'\"plan\"\\s*:\\s*\\{', t) else 'no')")
  MAT=$(python3 -c "import re; t=open('/tmp/dogfood-plan.json').read(); m=re.search(r'\"materialized_at\"\\s*:\\s*(null|\"[^\"]+\")', t); print(m.group(1) if m else '?')")
  PV=$(docker exec multica-postgres-1 psql -U multica -d "$DB" -tAc \
    "SELECT coalesce(state,'none') FROM plan_verification WHERE issue_id='$IID' ORDER BY created_at DESC LIMIT 1;")
  CH=$(mc issue children "$IDENT" --output json 2>/dev/null | python3 -c "import re,sys; m=re.search(r'\"total\"\\s*:\\s*(\\d+)', sys.stdin.read()); print(m.group(1) if m else '?')")
  echo "[$i] plan=$PLAN mat=$MAT children=$CH verification=$PV"
  if [[ "$PLAN" == yes && "$PV" == reported ]]; then
    break
  fi
  sleep 5
done

echo
echo "RESULT issue=$IDENT plan=$PLAN materialized_at=$MAT children=$CH verification=$PV"
if [[ "$TRUST" == approval && "$MAT" != null ]]; then
  echo "FAIL: expected materialized_at=null under trust_mode=approval" >&2
  exit 1
fi
if [[ "$TRUST" == approval && "$CH" != 0 ]]; then
  echo "FAIL: expected children.total=0 under trust_mode=approval" >&2
  exit 1
fi
if [[ "$PLAN" != yes || "$PV" != reported ]]; then
  echo "FAIL: expected plan + reported verification" >&2
  exit 1
fi
echo "PASS"
