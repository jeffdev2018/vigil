# Dated cycles (F29)

A cycle is one project's time-boxed iteration: a name, a start and an end
date, an optional human capacity and agent capacity, and the issues planned
into it. Cycles of one project may overlap (a support cycle beside a feature
cycle) — nothing forbids two open at once.

## Which cycle an issue may join

A cycle's issues are its project's issues. Naming a cycle of another project
is refused with `409 cycle_project_mismatch`, and so is naming one for an
issue that has no project. Move the issue first, or pick a cycle of the
project it is already in.

## What you may do

- Read: `GET /api/cycles?project_id=<id>&status=active|upcoming|closed` and
  `GET /api/cycles/{id}`. `status` is derived from the dates and the close,
  never stored: a cycle becomes active because the calendar turned over.
- Plan an issue in or out: `PUT /api/issues/{id}` with `{"cycle_id": "<uuid>"}`,
  or `{"cycle_id": null}` to take it out. `PATCH /api/issues/batch` accepts the
  same field under `updates` and reports per-issue refusals in `refused`.
- Read the burndown: `GET /api/cycles/{id}/burndown`.

Both writes take a task token. Unlike a goal, you may set a cycle yourself —
planning an issue into the iteration it is already being worked in is
bookkeeping, not a change of intent.

## What the numbers mean

- `load_unit` is `"issues"` or `"property"`. Without a load property on the
  cycle, one issue is one unit; with one, the unit is that number property's
  value per issue, and an issue whose value is missing or not a number counts
  zero. Never present a load as story points unless `load_unit` says
  `"property"`.
- `capacity.human` and `capacity.agent` are SEPARATE pools, split by the
  assignee: a member loads the human side, an agent or a squad loads the agent
  side. Work with no assignee loads neither and is reported as
  `unassigned_load`. A null `capacity` means nobody declared one — that is not
  zero, and it is not over capacity.
- `done_count` follows the workspace's status catalogue, so a custom status in
  the `done` category counts. Never decide doneness by comparing to the literal
  `"done"`.
- The burndown has one entry per calendar day from start to end. Days in the
  future carry the ideal line only (`remaining_count` is null). Today is
  computed live. Past days come from daily snapshots; a day with no snapshot
  carries the previous one forward, and when `approximate_before` is present,
  every day before that date is a flat fill, not observed history. Say so
  rather than reading a trend into it.

## What happens at the end

When a cycle's end date passes and nobody closed it, the platform closes it
and — unless the cycle was created with `rollover: false` — moves its
unfinished issues to the next cycle of the same project (the nearest one
starting later). Each move is journaled on the issue as `cycle_rolled_over`.
When there is no next cycle the issues are taken out of any cycle and the
project lead is told. Closing is one-way: a closed cycle's burndown is frozen.

## What you may not do

- Create, edit, delete or close a cycle. Those are member endpoints, gated on
  project write access.
- Assume an unfinished issue stays where you left it: a cycle that ends moves
  it. Re-read the issue rather than caching its `cycle_id` across a run.
