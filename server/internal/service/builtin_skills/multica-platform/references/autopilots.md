# Autopilots

An autopilot is not an agent. It is a rule that dispatches work to an agent, or
to a squad's leader agent.

## Core model

The chain is: trigger fires (`schedule`, `webhook`, or `manual`) → an autopilot
run is recorded → `execution_mode` decides the output → assignee readiness check
→ issue/task execution → run status sync.

Webhooks have a durable admission step in front: HTTP ingress stores a queued
webhook delivery, synchronously creates or reuses its idempotent run, and
returns `200` with `status=accepted|skipped` plus `run_id`; a leased worker then
resumes accepted runs and owns recoverable issue/task dispatch.

Execution modes:

- `create_issue` creates a Multica issue, making the run visible as issue state.
- `run_only` creates an agent task directly. No issue is created; any durable
  report location has to come from other task context or instructions.

`issue-title-template` only supports `{{date}}`. Do not invent `{{trigger_id}}`,
`{{branch}}`, or other variables.

## Off-peak dispatch

An autopilot with `batch_eligible` true declares its work non-urgent. When a
SCHEDULE trigger of such an autopilot fires inside the workspace's off-peak
window (`GET/PUT /api/batch-window`: `enabled`, `start_local_time`,
`end_local_time`, `timezone` — admin-only to write), the task it enqueues is
stamped `dispatch_lane: "batch"` and is claimed only after every `sync` task of
the same agent and runtime. It is a delay, not a cap: once nothing synchronous
is waiting, the batch task runs normally.

Only schedule-fired runs are affected. A manual `trigger`, a webhook delivery
and an API dispatch are requests for work now and always take the sync lane. So
"my nightly autopilot started at 00:40 instead of 22:00" is the lane working,
not a fault — check `dispatch_lane` on the run before hunting for a stall.

## CLI

```bash
multica autopilot list --output json
multica autopilot get <autopilot-id> --output json
multica autopilot create --title "<title>" --description "<task prompt>" --agent <agent-name-or-id> --mode create_issue|run_only --output json
multica autopilot update <autopilot-id> --status active|paused --output json
multica autopilot runs <autopilot-id> --output json
multica autopilot trigger-add <autopilot-id> --kind schedule --cron "0 9 * * *" --timezone Asia/Shanghai --output json
multica autopilot trigger-add <autopilot-id> --kind webhook --label "ci" --output json
multica autopilot trigger-add <autopilot-id> --kind schedule --cron "0 8 * * *" --window-minutes 120 --output json
multica autopilot trigger-add <autopilot-id> --kind webhook --event-match-criteria "only production incidents" --output json
multica autopilot trigger-update <autopilot-id> <trigger-id> --window-minutes 0 --output json
multica autopilot trigger-dry-run <autopilot-id> <trigger-id> --payload-file event.json --output json
multica autopilot trigger-dry-run <autopilot-id> <trigger-id> --output json
multica autopilot trigger <autopilot-id> --output json
multica autopilot trigger-rotate-url <autopilot-id> <trigger-id> --yes --output json
```

Do not run `trigger`, `delete`, `trigger-delete`, or `trigger-rotate-url` to
test — those are real side effects. Use `trigger` only when the user explicitly
asks for a manual run, and `trigger-rotate-url` only when rotating a webhook
URL; the old URL stops being valid immediately.

`trigger` exits non-zero when the run did not start. Only `issue_created` and
`running` mean work was dispatched; a `skipped` run — admission refused, runtime
offline, quota exhausted, a duplicate already in flight — dispatched nothing, and
its `failure_reason` says which. Do not report a manual run as done without
seeing one of those two statuses.

A schedule trigger without `--timezone` runs in **UTC**. Name the zone whenever
a human confirmed a wall-clock time, or they will confirm a morning job and
receive an afternoon one.

Trigger-kind-specific flags: `--window-minutes` (0-1439) is schedule-only and
spreads the firing over a band starting at the cron time;
`--event-match-criteria` is webhook-only and is judged per delivery by a
language model. Passing either to the other kind is rejected before the
request. On `trigger-update`, a flag is sent only when it was given, and an
empty `--event-match-criteria` is how the rule is cleared.

`trigger-dry-run` is the safe way to answer "would this fire". With
`--payload-file` it replays a sample event through a webhook trigger's whole
decision — event filters, `event_match_criteria` (the classifier really is
called, so it costs an upstream call), pause state, run quota — and prints
`{would_run, reason_code, explanation, matched_filters, event}`. Without it, a
schedule trigger's next five windowed occurrences are previewed. Neither writes
anything: no delivery row, no run, no `last_fired_at`. Prefer it over `trigger`
when the user is debugging routing rather than asking for a run.

`autopilot get` redacts `webhook_token`, `webhook_path`, and `webhook_url` by
default while reporting whether a token exists and its non-sensitive hint. Only
add `--show-secrets` when the user explicitly asks to retrieve the live webhook
credential; the command warns on stderr. Do not paste webhook tokens or signing
material into comments, logs, docs, or PRs.

## Debugging "why didn't it run"

1. `multica autopilot get <id> --output json` — status, mode, assignee, triggers.
2. `multica autopilot runs <id> --output json` — run status and failure reason.
3. If assigned to a squad, inspect the squad: `multica squad get <squad-id> --output json`; execution goes to the leader.
4. Inspect the target agent/runtime: `multica agent get <agent-id> --output json` and `multica runtime list --output json`.
5. For webhooks, inspect delivery status: `queued` means the worker has not completed dispatch; `failed` carries the worker error. A provider retry with the same `X-GitHub-Delivery` / `Idempotency-Key` reuses the original delivery. An `ignored` delivery carries a `reason_code` naming which step turned it away (`trigger_disabled`, `autopilot_paused`, `autopilot_archived`, `event_filtered`, `criteria_not_matched`, `quota_exceeded`).
6. To test a routing rule without firing anything, run `trigger-dry-run` with the payload in question; its `reason_code` uses the same vocabulary as the delivery rows.
7. For `create_issue`, inspect the created issue if the run records one.

## Access

Reads (list / get / runs / deliveries) are open to any workspace member, but
`get` redacts the webhook token for callers without write access — the token
alone can trigger the autopilot.

Editing, deleting, triggering, replaying deliveries, and managing
triggers/webhook secrets require the autopilot's creator, a workspace
owner/admin, or an explicit collaborator. Granting or revoking a collaborator is
narrower still: creator or workspace owner/admin only, so a granted collaborator
keeps write/execute but cannot re-grant or revoke peers. `get` stamps two
per-caller booleans — `can_write` and the narrower `can_manage_access` — read
those rather than inferring from role.

When YOU do any of this — `trigger`, `create`, `update`, `delete`, the
`trigger-*` commands, or a webhook-secret or collaborator change over the API —
the request is authorized as the human who asked you to (your run's originator),
not as the owner of the machine you execute on, whose grants are not consulted.
So the person who gave you the instruction needs that write access, and you
cannot change or trigger an autopilot they could not themselves. The same human
is stamped into what you write: an autopilot you create is theirs, and a trigger
you add fires as them for as long as it exists.

A run that carries no originator cannot write here at all: the server
answers `403` naming the missing originator rather than failing quietly, and the
CLI prints that reason instead of a generic permission error. Reads are
unaffected — except that `get` redacts the webhook token unless the human you
act for may write, since holding that token is equivalent to being able to
trigger.

## Side effects

These mutate durable state or start work: `create`, `update`, `delete`, trigger
add/update/delete/rotate, `trigger`, and webhook calls to
`/api/webhooks/autopilots/{token}`.

`trigger-dry-run` is NOT one of them — it records nothing. It does spend one
classifier call when the trigger has an `event_match_criteria`.
