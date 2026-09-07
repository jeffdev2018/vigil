# Bug-fix recipe pilot — 7 septembre 2026

Real Multica path on local env `vigil-482` (API `:18562`, daemon `dev-vigil-482`), not the provider-free `prove.mjs` fixture alone.

## Setup

| Item | Value |
| --- | --- |
| Workspace | `dev` (`9bcbf6e8-…`) |
| Project | Bug-fix recipe pilot (`517f91bf-…`) + `local_directory` → `/Users/jeff/multica_workspaces_dev-vigil-482/multica-bugfix-pilot` |
| Repo | https://github.com/jeffdev2018/multica-bugfix-pilot (private) |
| Agent | Bugfix recipe pilot (`c267cded-…`) on Claude runtime (`0c344870-…`, online) |
| Issue | **DEV-1** `01a07c51-59a3-7616-b398-10de52d45766` |
| Run | `01a07c51-59c0-7488-a7d2-af320c099c11` — completed in **57s**, exit 0, 11 tools |
| PR | https://github.com/jeffdev2018/multica-bugfix-pilot/pull/1 (title/body reference DEV-1) |
| Delivery review | accepted `62a03e12-e001-432d-b7ba-b03f94cbbb35` (human `dev@localhost`), delay **89s**, usage estimated **~$0.65** |

## Stages observed

1. **reproduce** — agent ran `npm test`, saw fail (`actual: -1`).
2. **fix** — `broken.js` now `return a + b` (removed `FIX_APPLIED` gate).
3. **test** — `npm test` 1/1 green in workdir after run.
4. **open_pr** — branch `fix/dev-1-add-returns-sum`, PR #1 via `gh`.
5. **delivery_review** — human Accept with per-criterion evidence.

## Limits (honest)

- Multica `pull_requests[]` stayed empty: no GitHub App install on this local workspace. PR evidence taken from GitHub URL + agent comment + local verification.
- Accept ≠ merge: PR left **open**; no deploy claimed.
- Activation self-pilot clock (daemon already up): inventory → assigned run complete ≈ **4 min**; accept review ≈ **6 min** from issue create. Not a cold-machine &lt;10 min study with an external team.
- Mobile native simulator captures: **blocked** — no Xcode/`simctl` on this host (Command Line Tools only). See [mobile-delivery-smoke-pilot-2026-09-07.md](mobile-delivery-smoke-pilot-2026-09-07.md).

## Commands used

```bash
multica --profile dev-vigil-482 …
# Delivery accept via POST /api/issues/{id}/delivery/reviews
```
