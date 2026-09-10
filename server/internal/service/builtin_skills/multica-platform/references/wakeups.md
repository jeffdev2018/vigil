# Scheduled wake-ups: follow-ups and autopilots from a sentence

Two ways to move work into the future.

- A **follow-up** fires **once**: a deferred run of *this issue's* agent, at a
  chosen instant, carrying a note that says what to do then. Use it to pause
  and resume instead of looping or idling.
- An **autopilot** fires **on a schedule, forever**. You can propose one from a
  plain sentence; it is created *paused* and a person turns it on.

If you are waiting on one thing at one time, schedule a follow-up. If the same
work should repeat, propose an autopilot instead of stacking wake-ups.

## Follow-ups

A follow-up is a row of the agent task queue with status `deferred`. It shows
on the issue, in the runs list (blocked on "deferred") and in the agenda. It
fires once, then it is an ordinary run whose trigger is your note.

Rules the server applies to every entry point:

- `when` is RFC 3339 (`2026-09-11T09:00:00+02:00`) or `+<minutes>` from now
  (`+90`). It must land between **1 minute** and **30 days** from now.
- `note` is at most 500 characters. Empty becomes "Scheduled follow-up.". It
  becomes the run's trigger summary, so write what the next run should *do*,
  not what you just did.
- **Budget**: by default **20 per agent per day** and **200 per workspace per
  day**, counted over the last 24 hours at schedule time. A workspace can move
  both between 1 and 1000 in `settings.followups`. Spending it answers 429 with
  the bound named; the fix is to cancel a pending follow-up, not to retry.
- Anyone who can see the issue can cancel one. A follow-up that already fired
  or was cancelled answers 409.

### Tools

- Native runtime: `schedule_followup {when, note}`, `cancel_followup
  {followup_task_id}`, `list_followups {}` — all on **this run's own issue**.
  Call `list_followups` before scheduling: a second wake-up on top of the first
  wakes the agent twice for the same reason.
- MCP: the `vigil_issue` compound tool — actions `followup`, `followups`,
  `followup_cancel` (granular names `issue_followup`, `issue_followups`,
  `issue_followup_cancel`). `followup` and `followup_cancel` are internal
  writes; `followups` is a read.
- CLI:

  ```
  multica issue followup <issue> --when "+90" --note "check the deploy"
  multica issue followup <issue> --when 2026-09-11T09:00:00+02:00 --agent CodeBot
  multica issue followups <issue>            # ids, fire times, notes, budget
  multica issue followup-cancel <issue> <followup-id>
  ```

  `--agent` is required only when the issue is not assigned to an agent; a run
  always schedules itself. `--output json` gives the raw rows.

The workspace agenda carries them: `multica calendar agenda --from … --to …`
and `GET /api/calendar/agenda` return a `followups` array alongside events,
due dates and cycles, so a wake-up is visible next to everything else dated.

## Autopilots from a sentence

`propose_autopilot` turns "every Monday at 9, list the open tickets" into a
title, a 5-field cron, an IANA timezone and the instruction each run follows.
Two steps, and the second one is not yours:

1. **Draft** — the model reads the sentence and the schedule is validated
   against the same cron parser the scheduler uses. Writes nothing.
2. **Propose** — the autopilot is created with status `paused` and its schedule
   trigger `enabled = false`. When there is an issue, a **Decision Card** is
   filed on it with options `autopilot:activate:<id>` and
   `autopilot:discard:<id>`. Activating flips the status to `active` and
   enables the trigger with its next run; discarding archives it.

A **member** may pass `activate: true` (CLI `--activate`) to skip the card and
create it running: the person deciding is the caller. A run asking for it is
refused with 403 — you propose, a person decides.

**Nothing runs until a person answers.** Propose, say what you proposed, and
stop. Do not propose the same automation twice because the card is still open.

Without a model configured the draft answers 503; pass `title`,
`cron_expression` and `description` yourself, or say the workspace has no model
and leave it to a person.

### Tools

- Native runtime: `propose_autopilot {text | title + cron_expression +
  description, timezone?, execution_mode?}` on this run's issue.
- MCP: the `vigil_autopilot` compound tool — actions `draft` (a read: it writes
  nothing) and `propose` (an internal write). Granular names `autopilot_draft`,
  `autopilot_propose`.
- CLI:

  ```
  multica autopilot draft "every Monday at 9, list the open tickets"
  multica autopilot draft "..." --create --agent CodeBot --issue ONE-42
  multica autopilot draft "..." --activate --agent CodeBot   # member only
  ```

  Without `--create` the draft is printed and nothing is written. With
  `--create` the autopilot is filed paused; `--agent` names who will run it and
  `--issue` decides where the Decision Card goes. Without an issue there is no
  card, and a person enables it from the Autopilots page (or with `autopilot
  update --status active` plus `autopilot trigger-update --enabled`).
- Chat: a member can type `/schedule every Monday at 9, list the open tickets`
  in a connected Slack / Telegram / Lark / WeCom / DingTalk channel. There is
  no issue behind a chat message, so there is no card: the autopilot is filed
  paused and the reply says what was understood and where to activate it.

`execution_mode` decides what a run is: `create_issue` opens an issue each time
(with `issue_title_template`, where `{{date}}` is the only token interpolated),
`run_only` just runs the instruction. Ask for `create_issue` when the result is
something the team should track, `run_only` when it is an action.

Read `references/autopilots.md` for what an autopilot does once it is active:
triggers, why one did not fire, and its run history.
