# Daemons declared in Markdown

A `DAEMON.md` is an autopilot written as a file: one declaration that projects
onto an autopilot, its triggers, and one skill bound to its agent. Nothing new
is stored — the same rows a UI-created autopilot uses. Read `autopilots.md`
first for what a run actually does; this file covers the file format, the
import contract, and the execution memory a daemon keeps between runs.

## The format

```markdown
---
name: Nightly triage           # required. Becomes the autopilot title AND the skill name.
role: |                        # required. Becomes the autopilot description — the run prompt.
  Sort yesterday's inbound issues and label them.
agent: Nova                    # required. Agent name (case-insensitive) or agent id.
outputs: issue                 # issue | run_only. Default issue. Maps to execution_mode.
issue_title_template: "Triage {{date}}"
triggers:                      # optional. Omit for a daemon only ever run by hand.
  - kind: schedule             # schedule | webhook. Nothing else.
    cron: "0 9 * * *"          # required for schedule, rejected on webhook
    timezone: Europe/Paris     # optional, default UTC, rejected on webhook
    label: Morning             # optional
  - kind: webhook
    label: CI
budget:                        # optional, and see the warning below
  runs_per_day: 3
  max_minutes: 20
---

Everything below the closing --- is the SKILL the agent reads on every run.
```

The frontmatter is validated strictly: an unknown key is a `400`, not a
shrug. `triggerss:` must fail loudly, or you ship a daemon that never fires
while its author believes it is scheduled. Every rejection carries the line it
happened on, and a rejected document writes nothing at all.

`kind: api` is retired. It was never dispatched by anything, so accepting one
would declare a trigger that can never fire. Use `schedule`, `webhook`, or run
the daemon by hand.

`budget` parses, is stored, and comes back on export — but nothing enforces it.
Run quota is workspace-wide, tied to the plan, and there is no per-autopilot
limit to write `runs_per_day` into. The import says so in its `warnings`.
Do not tell a user their daemon is capped at three runs a day.

## Importing

```bash
# parse only, no write — reports the frontmatter, the resolved agent and line errors
multica autopilot import DAEMON.md --preview

# strategy: fail (default) | overwrite | rename
multica autopilot import DAEMON.md --strategy overwrite
```

Over HTTP the same two steps are `POST /api/autopilots/import` and
`POST /api/autopilots/import/preview`, with `{markdown, strategy}` as a JSON
body or the file in a multipart `file` field.

Three things about import worth knowing before you use it:

- **Re-importing the same bytes does nothing.** The document's digest is
  compared first, so an unchanged declaration is a no-op that does not even
  move `updated_at` — whatever `strategy` says, because nothing conflicts.
- **It replaces, it does not merge.** The declared trigger list becomes the
  whole trigger list. A schedule you deleted from the file stops firing. That
  is the point of a declaration; merging would leave it alive.
- **A name is the identity.** `name` is matched against the autopilot title.
  `strategy=fail` returns `409 daemon_name_conflict`, `overwrite` replaces,
  `rename` creates `<name>-2`.

An unknown `agent` is `404 daemon_agent_not_found`, and the message lists the
agents the workspace does have — the fix is usually a typo.

## Exporting

```bash
multica autopilot export <autopilot-id> --out DAEMON.md
```

A daemon that was imported exports its source **verbatim**, so your comments
and your `budget` block survive the round-trip. An autopilot that was created
in the UI gets one rendered from its current configuration, which is how an
existing autopilot gets pulled into a repository for the first time.

## Execution memory

A daemon keeps one memory document, scoped to the daemon — not to the agent.
The same agent can serve several daemons, and what a nightly triage job learned
about its own queue has no business in an unrelated run.

```bash
multica autopilot memory get <autopilot-id>
multica autopilot memory set <autopilot-id> --content "..."   # or pipe on stdin
```

Rules that bite:

- **Only a run of that daemon may write it.** The write is authorized by your
  run's task token, so it works from inside the run and nowhere else. Members
  read it; they cannot write it.
- **It is a whole document, not a list.** `set` REPLACES. Read it first,
  edit, write the result back — the CLI sends `If-Match` with the revision it
  read, and a `409 memory_revision_stale` means another run of the same daemon
  wrote in between. Re-read and re-apply; do not retry the same body.
- **Over 8 KiB is cut, not refused.** The cut lands on a line boundary and the
  response reports `truncated`. Keep the note short enough that this never
  matters.
- **What you write is read as DATA.** The next run's brief tells it your note
  is a report, not instructions — it cannot grant permissions or hand out
  tasks. Write facts the next run needs ("the queue label is `inbound`, not
  `inbox`"), not orders ("delete the stale branches"). Orders will be ignored,
  and should be.

Write durable state, not a run log. Nothing about what you did this time,
nothing that will be false next week.
