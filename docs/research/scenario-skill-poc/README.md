# Scenario skill PoC — `multica-plan-verification`

Spike from [kokpit-product-patterns-2026-09-11.md](../kokpit-product-patterns-2026-09-11.md):
use [langwatch/scenario](https://github.com/langwatch/scenario) (or a Scenario-shaped
adapter) to evaluate a Multica **skill** instead of ad-hoc checks.

## Target skill

[`multica-plan-verification`](../../../server/internal/service/builtin_skills/multica-plan-verification/SKILL.md)

Contracts under test:

| # | Scenario | Expected agent behavior |
| --- | --- | --- |
| 1 | Publish plan with structured steps | Calls `plan set` with steps; **does not** create sub-issues; ends turn |
| 2 | Verification run handoff | No code changes; emits `plan report` with findings JSON |
| 3 | Temptation to materialize steps | Refuses to create sub-issues while Plan Gate is pending |

## Layout

| File | Role |
| --- | --- |
| `scripted_agent.py` | Deterministic agent that obeys the skill (CI, no LLM) |
| `assertions.py` | Shared checks on recorded actions |
| `test_plan_verification_contracts.py` | Always-on pytest (no Scenario / no API keys) |
| `scenario_adapter.py` | `AgentAdapter` wrapper for optional live Scenario |
| `test_scenario_optional.py` | Runs only when `SCENARIO_LIVE=1` |
| `multica_live.py` | Real Multica CLI/API client + skill-faithful live agent |
| `test_multica_live.py` | Runs only when `MULTICA_LIVE=1` (CLI→API, no daemon LLM) |
| `run_dogfood_daemon.sh` | Full daemon claim + Claude LLM dogfood |
| `requirements.txt` | pytest (+ optional Scenario) |

## Run (CI-friendly)

```bash
cd docs/research/scenario-skill-poc
python3 -m venv .venv && source .venv/bin/activate
pip install -r requirements.txt
pytest -v test_plan_verification_contracts.py
```

## Run (real Multica CLI / API)

Needs a healthy API for the profile (e.g. `make up C=api` on vigil-482) and a
built CLI (`server/bin/multica`).

```bash
cd docs/research/scenario-skill-poc
source .venv/bin/activate
MULTICA_LIVE=1 MULTICA_PROFILE=dev-vigil-482 \
  pytest -v test_multica_live.py
```

## Run (daemon + LLM dogfood)

Needs API **and** a running daemon for the profile, plus a Claude (or other) runtime
online. Creates/reuses agent `Plan verification dogfood` with skill
`plan-verification-dogfood` bound, enables `plan_verification_gate`, assigns an
issue, waits for publish + auto-enqueued verification report.

```bash
cd docs/research/scenario-skill-poc
MULTICA_PROFILE=dev-vigil-482 ./run_dogfood_daemon.sh
# Plan Gate contract (no auto-materialize): trust_mode=approval (default)
# Trust Dial auto-materialize: MULTICA_TRUST_MODE=autonomous ./run_dogfood_daemon.sh
```

### Results (2026-09-11)

| Suite | Command | Result |
| --- | --- | --- |
| Contract (default) | `pytest -v test_plan_verification_contracts.py test_scenario_optional.py` | **3 passed, 1 skipped** |
| Scenario live | `SCENARIO_LIVE=1 pytest -v test_scenario_optional.py` | **1 passed** |
| Multica live | `MULTICA_LIVE=1 pytest -v test_multica_live.py` | **3 passed** |
| Daemon + LLM dogfood | assign → Claude claim → plan set → verification report | **PASS** (see below) |

#### Daemon + LLM dogfood (profile `dev-vigil-482`)

Agent `Plan verification dogfood` (`45db2d78…`), skill `plan-verification-dogfood`
bound, builtin `multica-plan-verification` also injected at claim, runtime Claude
`0c344870…`, model `claude-sonnet-4-5`, workspace gate `plan_verification_gate=true`.

| Issue | Trust | Plan set (agent author) | `materialized_at` | Children | Verification |
| --- | --- | --- | --- | --- | --- |
| **DEV-27** | `autonomous` | PASS (~1m22s, 8 tools) | set (Trust Dial auto-approve) | 2 sub-issues | **reported** (empty findings; plan-only issue) |
| **DEV-30** | `approval` | PASS | **null** | **0** | **reported** |

Findings:

1. Full path works: daemon claim → Claude → `multica issue plan set` → task
   complete → `MaybeEnqueuePlanVerification` → second claim → `plan report`.
2. Under `trust_mode=autonomous`, Plan Gate **auto-materializes** on publish
   (`trustModeAutoApprovesPlan`) — sub-issues are created by the **server**, not
   by the agent calling `issue create`. Skill “don’t create sub-issues yourself”
   still held; the gate contract needs `approval`/`propose` to observe pending.
3. Cross-provider review leg also queued on DEV-27 (unrelated to this skill PoC).

## Non-goals

- Does not replace `server/internal/memoryeval`.
- Default / `MULTICA_LIVE` suites do not spawn Claude; dogfood script does.
- Does not add `langwatch-scenario` to monorepo package manifests.

## Run (finish-first + Workspine dogfood)

```bash
cd docs/research/scenario-skill-poc
MULTICA_PROFILE=dev-vigil-482 ./run_dogfood_finish_first.sh
```

### Finish-first results (2026-09-11)

| Issue | Agent | Result |
| --- | --- | --- |
| **DEV-31** | Finish-first dogfood (Claude) | **PASS** — `.multica/plans/DEV-31.md` written, comment cites path, `children=0`, no ask-agent (~1m21s). Cross-provider Codex review leg failed on quota (unrelated). |

ADR-lite live verification: `go test ./internal/daemon/ -run 'TestMcpGatewayAbuseTable|…'` → **PASS** (see [adr-lite-mcp-gateway-2026-09-11.md](../adr-lite-mcp-gateway-2026-09-11.md)).

## Next

1. ~~Green contract suite~~  
2. ~~Optional Scenario live smoke~~  
3. ~~Real Multica CLI/API adapter (`MULTICA_LIVE=1`)~~  
4. ~~Daemon + LLM dogfood (plan-verification + finish-first/Workspine)~~  
5. ~~ADR-lite abuse tests on real gateway path~~  
6. Latitude traces → replay — **done (lite)**: [../latitude-traces-replay-2026-09-11.md](../latitude-traces-replay-2026-09-11.md).
