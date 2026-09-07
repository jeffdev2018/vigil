# Project memory reuse measurement — 7 septembre 2026

## Product

GET `/api/projects/{id}/memory/usage` mirrors agent memory usage for **project**
rules prepared into claim context (`memory_context.project_version`):

- Window: last 30 days
- Scope: started, retained, non-chat runs on issues of the project
- Honest: counts prepared context, not daemon receipt or model adherence
- UI: **Memory across runs** on the project memory card (en/zh/ja/ko)

## Pilot on vigil-482

| Step | Result |
| --- | --- |
| Publish rules on Bug-fix recipe pilot | revision **1** |
| Issue **DEV-2** assigned to Bugfix recipe pilot (Claude) | completed ~24s |
| `memory_context.project_version` | `{ id: 517f91bf-…, revision: 1 }` |
| Agent comment | quoted both rules verbatim |
| Usage API | `started_runs: 2`, `runs_with_project_memory: 1`, revision 1 `prepared_runs: 1` |

DEV-1 (pre-publication) remains the run without project memory in the same window.

## Limits

- Does **not** prove learning success or fewer corrections.
- Agent-memory reuse metrics already existed; this closes the project gap.
- Autonomous learning / skill replay remain separate ledger rows.
