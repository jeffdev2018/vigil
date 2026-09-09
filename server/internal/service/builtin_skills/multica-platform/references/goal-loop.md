# The goal loop: long tasks across runs

A run on an issue ends on a closing status; the issue is rarely finished. The
server keeps a **goal** per issue and, after every settled run on it, a judge
reads your closing status against that goal and decides what happens next.
You do not drive the loop; you work one run at a time and leave a status the
judge can read.

## What the judge decides

After your run completes, a tool-less model reads the goal (the team's written
definition of done when there is one, else the issue's title, description and
acceptance criteria), the evidence gathered by the previous runs of the chain,
and your closing status. It answers `satisfied` or one of five blockers:

| Blocker | What it means | What the loop does |
|---|---|---|
| `goal_not_met_yet` | real progress, work remains | queues the next run on the issue |
| `missing_evidence` | you said "done" without saying what was done | queues the next run |
| `needs_user_input` | a decision only a human can make | asks the team, waits |
| `external_wait` | waiting on something outside the workspace | stops, tells the team |
| `run_failed` | the run stopped on an error or produced nothing usable | stops, tells the team |

`satisfied` moves the issue to **done** through the workspace's transition
rules, with you as the actor: a rule that requires approval files a request
instead of moving the issue. The chain also stops after **8 continuations**
(the workspace can set 1–20) and when two continuations in a row end on the
same status (stagnation).

## What you get on the next run

The goal state is rendered in your prompt under `Goal state for this issue`:
the goal, the continuation number, why the previous run was not enough, the
next step the judge suggested, the evidence so far, and the answer to any
question you asked. It is the chain's memory; read it before working and do
not redo what the evidence already covers. The handoff note on the task
carries the previous run's closing status.

## Writing a closing status the judge can use

End every run with three parts: what you did (with concrete evidence — what
changed, what was filed, what was verified), what remains, what blocks you.
A status without evidence is `missing_evidence`, which costs the team a run.

## Asking the team

When you need a decision only a human can make, ask it and stop:

- Native runtime: the `ask_user` tool (`question`, `kind` = `text` | `choice`,
  `options` for choice).
- CLI runtime: `POST /api/issues/{issue}/goal/question` with your task
  credential (`X-Task-ID` is your run) and `{"question": "...", "kind":
  "text"|"choice", "options": [...]}`.

The run ends on `needs_user_input`; the team gets an inbox item and a comment
on the issue; a follow-up run is queued with the answer once they reply. Ask
only what the issue does not settle: a question is a run spent waiting.

## Reading the state

`GET /api/issues/{issue}/goal` returns `{"goal": {...}}` — `status`
(`active`, `paused`, `waiting_user`, `satisfied`, `stopped`), `continuation`,
`max_continuations`, `last_blocker`, `last_reason`, `next_step`, `evidence`,
`question`, `chain_root_task_id` (the legs endpoint keyed on it totals what
the chain cost). `null` when the issue has never been judged nor given a goal.

Members write the goal (`PUT /api/issues/{issue}/goal` with `goal` and
`max_continuations`), pause and resume the chain (`POST .../goal/pause`,
`.../goal/resume`), and answer your questions (`POST .../goal/answer`). A
paused chain judges nothing and queues nothing until resumed; a resume grants
a fresh allowance and queues a run from the goal state.

Workspace setting `goal_loop`: `max_continuations` (0 turns the judge off),
`propose_done` (false records the verdict without moving the issue).
