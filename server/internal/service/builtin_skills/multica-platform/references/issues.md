# Issues

Product contracts the runtime brief does not fully encode.

- [PR linking and close intent are two distinct contracts](#pr-linking-and-close-intent-are-two-distinct-contracts)
- [Reading a linked PR's real state](#reading-a-linked-prs-real-state)
- [Editing comments without overwriting concurrent work](#editing-comments-without-overwriting-concurrent-work)
- [Custom properties: typed workflow state](#custom-properties-typed-workflow-state)
- [Status changes have server side effects](#status-changes-have-server-side-effects)
- [Claim ownership without duplicating a run](#claim-ownership-without-duplicating-a-run)
- [The delegate is not a second assignee](#the-delegate-is-not-a-second-assignee)
- [Who else is running right now](#who-else-is-running-right-now)
- [Sub-issues: todo starts work now, backlog parks it](#sub-issues-todo-starts-work-now-backlog-parks-it)
- [Incorrect to correct](#incorrect-to-correct)

## Editing comments without overwriting concurrent work

Read the comment's current `revision`, then supply that positive value when
updating its body. Agent-authored bodies must use `--content-file`.

```bash
multica issue comment list <issue-id> --output json
multica issue comment update <comment-id> --content-file ./comment.md --expected-revision <revision>
```

If another editor changed the comment, the server rejects the stale revision.
Read the latest body and reconcile the edits before retrying; do not simply
advance the revision and overwrite the other edit. Authors can edit their own
comments; workspace owners and admins can edit any comment. Existing attachments
remain unchanged. Content edits have the same agent-trigger behavior as edits
in the app, so do not use an update as a silent bookkeeping operation.

## PR linking and close intent are two distinct contracts

The GitHub webhook runs two separate scans over an incoming PR. They are not the
same gate and they read different fields.

**Linking** scans three places for a routable issue key (`PREFIX-NUMBER`, e.g.
`MUL-123`): the PR **title**, the **branch name**, and the **body right after a
closing keyword**. Each match writes an issue to PR link row — the link that
`multica issue pull-requests` reads back. A key that appears in the body as a
bare mention, with nothing in the title or branch and no closing keyword, is a
passing reference and links nothing.

```text
MUL-123: add the thing the issue asks for        # key anywhere in title → links
agent/dana/mul-123-add-the-thing             # branch ref   → links
Closes MUL-123                                   # body + closing keyword → links
Related to MUL-123                               # body mention only → no link
```

**Close intent** is stricter and is a separate scan over **title or body only —
never the branch**. It fires only for a key placed immediately after a closing
keyword (`Closes` / `Fixes` / `Resolves`, optional `:` then whitespace). That
adjacency is what sets the link row's close-intent flag, the gate that
auto-advances the issue to `done` when the PR merges.

```text
Closes MUL-123                                    # links AND records close intent
Fixes MUL-123
Resolves MUL-123
Fix login MUL-123                                 # in a title: links, no close intent
```

Consequence: a bare key in the title or a branch reference links the PR but does
not close the issue on merge. A closing keyword immediately adjacent to the issue key
records close intent; on merge, that close intent can move the linked issue to
`done`.

**Passing mentions link nothing.** A key that appears **only** as a bare mention
in the body — no closing keyword, and not in the title or branch — does not link
the PR at all. This keeps `Related MUL-123` or `Follow up in MUL-123` from
surfacing an unrelated PR as if it were working on that issue. To make a PR show
up for an issue, put the key in the title, the branch, or after a closing keyword
in the body — not as a loose body reference.

While the PR is still open the link follows the live title and body: adding a key
links it, and downgrading that key to a plain mention drops the link. Once the PR
has merged or closed, existing links and their close-intent decision are frozen —
but a PR that was never linked can still be linked by editing it, so a forgotten
key is repairable after the fact. That late link does not move the issue to
`done`; close intent is decided at merge time.

```text
Closes MUL-123 in the body                        # links
Related to MUL-123 in the body (no title/branch)  # no link
```

### Default for code-changing issue work

When an issue run changes code in a checked-out GitHub repo, the default handoff
is to open or update a PR before posting the final Multica issue comment, unless
the user explicitly asked for a local-only change or no PR. This is a default, not
an unconditional command: if no code changed, say no PR is needed; if PR creation
is blocked by auth, failing tests, or missing remote state, report that blocker
instead of pretending the run is complete.

To make the PR show on the issue, put a routable issue key in the PR **title**
(preferred) or the **branch**. A key that appears only as a bare mention in the
body links nothing. Do not use a closing keyword (`Closes` / `Fixes` /
`Resolves`) unless the issue should auto-advance to `done` on merge.

```text
MUL-123: fix login redirect        # key anywhere in title → links
Closes MUL-123                     # only when merge should mark the issue done
Part of MUL-123                    # body mention only → no link at all
```

In the final issue comment, include the PR URL when a PR exists. If the task did
not produce a PR because no code changed or the user asked not to create one, say
that explicitly.

## Reading a linked PR's real state

When a step depends on PR state, query Multica's link table — do not infer it
from branch names, GitHub search, memory, or stale values left on the issue by
an earlier run.

```bash
multica issue pull-requests <issue-id> --output json
```

Returns `{"pull_requests": [...]}`. Each element exposes:

- `number`, `html_url`, `title`
- `state` — the PR lifecycle as a **single enum**, one of `merged`, `closed`,
  `draft`, `open`. There is no separate `draft` or `merged` boolean in the
  response; the server folds them into `state` (merged wins, then closed, then
  draft, else open).
- `merged_at` — non-null once merged; a second confirmation of `state: merged`.
- `provider` — `github`, `forgejo`, `gitea`, or `gitlab`.
- `mergeable_state` — mirrors GitHub (`clean` / `dirty` surfaced; other values
  round-trip as unknown; retained for compatibility).
- GitHub API snapshot fields: `snapshot_available`, `mergeable`,
  `merge_state_status`, `checks_rollup`, `checks_total`, `checks_passed`,
  `checks_failed`, `checks_running`, `failed_check_names`,
  `snapshot_fetched_at`, and `snapshot_stale`. `snapshot_available == true`
  means the feature is enabled and the snapshot matches the PR's current head.
  Only then does `checks_rollup == null` mean "no checks"; false means the
  snapshot feature is disabled, has not fetched yet, or only has an old head.
- `checks_conclusion` — coarse CI compatibility status: `passed`, `failed`,
  `pending`, or `null`. GitHub derives it from the current API snapshot;
  Forgejo/Gitea/GitLab derive it from webhook commit statuses. Backed by the
  provider-appropriate check counts.

So "is it merged?" is `state == "merged"` (or `merged_at != null`); "is it still
a draft?" is `state == "draft"`; coarse CI status is `checks_conclusion`.

If the command returns no linked PRs after a PR was opened, check the syntax
first: the scanner needs a routable issue key in the PR title or branch, or one
right after a closing keyword in the body — a bare body mention does not count
(see the passing-mention rule above). When the syntax is the problem, editing the
title or adding a closing keyword re-runs the scan.

If the key is already written correctly and the list is still empty, stop editing
the PR blind: another no-op edit cannot fix an integration that never received the
event. Check the integration side instead — whether the app is installed on that
repository, whether the installation is bound to this workspace, whether
auto-linking is turned off for the workspace, and whether the event reached the
platform at all. A delivery that failed is not retried on its own, but it can be
redelivered once the receiving side is fixed. Report what you found in the result
comment rather than repeating the edit.

## Listing and ordering issues

`issue list` reads one page at a time, with a server maximum of 100 issues.
Advance `--offset` by the number of issues actually returned. If the server
cannot count matching issues, it returns `failed to count issues` as an error;
do not treat that failure as an empty or complete list. Older servers can
substitute the page length for a failed count, so that value alone is not proof
that all matching issues have been read.

`issue reorder` reads the issue's project-scoped status column before writing
its new position. When a legacy total is unavailable or no larger than its
page, it reads through an empty page. A failed request, malformed page, or
duplicate issue stops the operation before any position write. This protects
against truncated or repeated pages, but does not promise a snapshot across
concurrent edits. There is no CLI bulk-export or `--all` mode.

## Custom properties: typed workflow state

See `references/issue-types.md`.

## Status changes have server side effects

A status change is not cosmetic — the server enqueues or skips agent work based
on it. These are the contracts, not advice.

The rules below name fixed built-in status keys, not category-wide behaviors.
Custom statuses have only lifecycle semantics: unstarted, started, done
(successful terminal), or closed (cancelled terminal). They do not inherit
Backlog parking, In Review completion, Blocked failure, or In Progress recovery.
Use the built-in key when its special behavior is needed. Built-in definitions
cannot be edited or archived.

Archive a custom status only after moving every issue off it, including
completed/canceled issues. An occupied status returns HTTP 409 with code
`issue_status_in_use` and `issue_count`; it remains active. Use Settings >
View issues to inspect and move its issues, then retry. For terminal-status
replacement, preserve the lifecycle meaning (`done` to `done`, `closed` to
`closed`); do not reopen or cancel completed work just to retire a status.
Archival does not move issues automatically. Historical issues on previously
archived statuses remain readable via an explicit status filter.

- **`backlog`** parks an agent-assigned issue: the assignee is set but no task
  fires. Moving `backlog → todo` (or any non-done/non-cancelled status) enqueues
  the assigned agent then.
- **`in_progress` / `in_review`** are agent-managed CLI mutations, not automatic
  side effects of a task starting or finishing. The runtime brief asks agents to
  write the state the issue is in whenever their work changes it — not from
  the trigger type or the run's lifecycle, and not gated on being the
  assignee. Writes happen whenever the state changes, mid-turn included: a
  turn that advances the issue's own ask sets `in_progress` as soon as that
  is known, so the board shows the work while it runs; a blocker is recorded
  when it is hit; and the turn must not exit with a stale value — delivered
  the issue's own ask → `in_review`; work continues beyond the turn
  (dispatched sub-issues, partial delivery) → `in_progress`; stuck →
  `blocked`. A turn that produces none of the issue's own deliverable —
  answering a question, consulting on work owned elsewhere — writes nothing
  at any point. The kind of activity never decides this: research, design,
  planning, and review all count as the work exactly when they are what the
  issue asks for (a review-the-PR issue is being worked the moment reviewing
  starts). Questions, discussion, or acknowledgements never move the status.
  Squad leaders: dispatching members is not delivery — a dispatch turn
  leaves the parent `in_progress`, and it moves to `in_review` only when a
  later re-trigger confirms the overall goal is met.
- **`in_review`** is an accepted issue status. Some workflows use it while a PR
  is open and awaiting review; moving to it is an explicit mutation.
- **`done`** on a child issue posts a system comment on its parent. If a PR
  carries close intent (`Closes MUL-XXXX`), it advances the issue to `done`
  itself on merge — you do not also need to flip it manually.
- **`cancelled`** is a terminal, user-driven decision to close the issue. Like
  `done` it enqueues no new agent work, but it does **not** stop tasks already in
  flight — a run in progress keeps going. To stop a running task, cancel the
  task itself.
- **Failed issue-triggered tasks** may roll an issue from `in_progress` back to
  `todo` when no active task / retry remains — that is the main server-owned
  status write on the agent-run path.

## Claim ownership without duplicating a run

Assigning an active issue to an agent normally starts a run. When the work is
already underway and the write only records ownership or progress, pass
`--no-start` on every command in that flow:

```bash
multica issue assign <issue-id> --to-id <agent-id> --no-start
multica issue update <issue-id> --assignee-id <agent-id> --no-start
multica issue status <issue-id> in_progress --no-start
```

Before self-assigning, check the target issue's comment history for an existing
claim. The server also suppresses a trusted self-assignment when the exact
target `(issue, agent)` pair already has a non-terminal task, but it
deliberately keeps same-agent handoffs to a fresh issue starting runs:
cross-issue serial chains and triage batches rely on that.

## The delegate is not a second assignee

`--delegate` / `--delegate-id` on `issue create` and `issue update` name the
assignee's partner. It is inert: naming one starts **no run** (even for an
agent) and carries **no status** — the assignee stays the only run trigger and
the only status writer. Member or agent only; never a squad, and never the
same actor as the assignee (both `400`).

```bash
multica issue update <issue-id> --delegate-id <member-or-agent-id>
```

## Who else is running right now

Nothing about concurrent runs is pushed into your prompt: the answer changes
while a turn is running, and most turns never need it. Ask the server on the
turns that do — before opening a PR against code a sibling issue also touches:

```bash
multica issue runs <issue-id> --active --output json     # in-flight runs on this issue
multica issue runs <issue-id> --siblings --output json   # ...and across the sub-issue family
```

`--active` drops the execution history and returns only `queued` / `dispatched`
/ `running` / `waiting_local_directory` runs. `--siblings` widens the same read
to the issue's family — its parent (or itself, when it has no parent) plus every
child of that parent — and labels each row with the issue it belongs to.

The family read returns a compact row — task, issue, agent, status, started —
not the full execution-log record. If you need a run's detail, follow the task
id with `multica issue run-messages`.

Rows come back running-first, newest-first within a status, and the family read
is capped at 20. When the cap truncates the answer the CLI prints a warning on
stderr — read it. Without that warning a short list means "nobody else is
there"; with it, the list proves nothing about the runs it did not return.

Both are advisory reads. Nothing here reserves an issue or serialises anything:
a run you see may finish a second later, and one you don't see may start a
second later. Coordinate through the issue's comments — the reads tell you whom
to coordinate with.

## Publishing your run plan

Publish the checklist you are working through so a reader sees where you are:
```bash
multica issue run-plan set "$MULTICA_TASK_ID" --item "Fix the parser:in_progress" --item "Update the docs:pending"
```

`--item` is `<text>:<status>` (last colon splits), status `pending|in_progress|done`, one `in_progress` at most, 1-30 items; JSON `{"items":[…]}` on stdin works too. Publishing REPLACES the plan: do it at milestones only. Only the run itself may publish (`403` otherwise, `409` once finished).

## Goal ancestry rides the brief

When the claimed issue has a parent, the brief carries `## Goal Ancestry`:
the parent chain from the top-level goal down, each with identifier, title,
description and acceptance criteria. Read it before `multica issue get` — it
is why your issue exists; stay consistent with it. Capped at five levels and
8 KiB (a `(N higher level(s) not shown.)` line says when cut); a root issue
gets no section; children still come from `multica issue children`.
Quick-create runs from "Add sub issue" get it for the parent. An issue serving
a workspace goal also gets `## Mission and goals`; you never set a goal, you
propose one via `POST /api/issues/{id}/goal-proposal` (`references/goals.md`).

## Before coding on a vague issue: the requirement interview

When the issue does not say enough to start — which behavior, which surface,
which of two readings — do not guess and do not start coding. Ask one to
three multiple-choice questions at once, then finish your turn:

```bash
multica interview ask <issue-id> --file questions.json
```

`questions.json`:

```json
{"questions": [
  {"question": "Should the export include archived issues?",
   "options": [{"id": "yes", "label": "Yes, everything"}, {"id": "no", "label": "Active issues only"}],
   "recommended_option_id": "no"},
  {"question": "Which format?",
   "options": [{"id": "csv", "label": "CSV"}, {"id": "json", "label": "JSON"}, {"id": "both", "label": "Both"}]}
]}
```

The issue moves to **Waiting for PM** until every question is answered; then
it returns to its previous status and you are resumed with a handoff note
that starts with `Requirement interview answered:` and lists each answer in
order. Multiple choice only: no open questions here (a single open decision is
`multica decision ask`). Ask only what the issue does not settle; a simple
issue needs no interview.

## Leaving a handoff packet

When you finish or pause, leave a structured handoff packet on the issue so
the next hand (an agent or a human) does not rebuild your context from the
transcript. `POST /api/issues/{issue}/handoff-packet` with `run_id` (your
task id), `objective`, `decisions`, `evidence`, `failed_attempts` and
`next_action`. Failed attempts matter most: they stop the next run from
repeating them. Packets are immutable; to correct one, post another. A run
that completes without one gets a system packet (objective, delivery pointer,
next action) so the chain never breaks. On your next claim the latest packet
is rendered at the top of your prompt; the legacy `handoff_note` still
appears beside it.

## Asking a human for a decision

When a choice is not yours to make — deleting data, picking between two
designs, spending real money, touching production — or when you are blocked
mid-run and cannot proceed without a human's answer, file a Decision Card
instead of guessing or stalling on a comment:

```bash
multica decision ask <issue-id> \
  --question "Drop the legacy orders_v1 table?" \
  --option "drop=Drop it|irreversible, frees 40 GB" \
  --option "keep=Keep it for now|no risk, migration stays partial" \
  --recommend keep --urgency high
```

Two to eight options with stable ids, one recommendation, an urgency of
`low`, `normal` or `high`. Then **finish your turn**: the human answers from
the issue, and Multica queues a new run on the issue whose handoff note starts
with `Decision on «…»` and names the chosen option (or the human's own text).
Read that note first and proceed. A card is never a substitute for reading
the issue; ask only what the issue does not already settle.

## Proving acceptance criteria

An issue can carry acceptance criteria, each with a proof. The issue cannot
move to `done` while a criterion lacks a satisfied proof: the status change is
refused with `409 unsatisfied_acceptance_criteria` naming the criteria. Read
them first, and attach a proof as you satisfy each one:

```bash
multica criteria list <issue-id>
multica criteria prove <issue-id> <criterion-id> --type test --ref "npm test -- export"
multica criteria prove <issue-id> <criterion-id> --type human_validation
```

`test`, `file`, `screenshot` and `url` proofs need a `--ref` naming what
proves it. A `human_validation` from a run only marks the criterion as
waiting for the human: their own click satisfies it, not your claim. If the
issue has no criteria yet and the task states some, set them with `multica
criteria set <issue-id> --text "..." --text "..."` before starting (refuses
with no --text at all; pass --clear to wipe the list).

## Sub-issues: todo starts work now, backlog parks it

On an agent-assigned issue, create status decides whether the assignee fires
immediately. A non-backlog status (e.g. `todo`) enqueues the agent at create
time; `backlog` sets the assignee without triggering.

Parallel children — all start now:

```bash
multica issue create --title "..." --parent <issue-id> --assignee <agent> --status todo
```

Strictly serial children — park later steps, promote one at a time:

```bash
multica issue create --title "Step 2: ..." --parent <issue-id> --assignee <agent> --status backlog
multica issue status <child-id> todo   # promote when the previous step is truly done
```

Creating every serial step as `todo` enqueues the whole chain at once.

Issues you create may be held for human triage: a workspace can gate the
`agent_create` source, and the API then answers `202` with
`{"code":"triage_held"}` and a queue entry id instead of `201` and an issue.
Nothing was lost — a human accepts it — but there is no issue id to work
against yet, so do not retry the create and do not treat the item id as one.

### Stages: order sub-issues into barrier groups

`--stage <N>` (N >= 1) groups sub-issues under the same parent into ordered
stages. The server **tries once to wake the parent assignee when a whole stage
finishes** — i.e. every sub-issue in the lowest unfinished stage has reached a
terminal status (`done`/`cancelled`); a notification that fails is not replayed.
A completion that does not close a stage is silent (no comment, no wake). A
sibling set with **no** stages is one implicit stage, so the parent is woken
once when the *last* sub-issue finishes — not on every child.

Advancement is agent-driven: the server only detects the closed barrier and
wakes the parent assignee, who then decides whether to promote the next stage's
`backlog` sub-issues to `todo`.

```bash
# Stage 1 runs now; later stages parked until promoted
multica issue create --title "Research A" --parent <id> --assignee <agent> --stage 1 --status todo
multica issue create --title "Research B" --parent <id> --assignee <agent> --stage 1 --status todo
multica issue create --title "Build"      --parent <id> --assignee <agent> --stage 2 --status backlog
multica issue create --title "Ship"       --parent <id> --assignee <agent> --stage 3 --status backlog
```

When both Stage 1 sub-issues finish you (the parent assignee) are woken with a
"Stage 1 complete" comment. Inspect the layout, then promote the next stage:

```bash
multica issue children <parent-id>             # sub-issues grouped by stage
multica issue status <stage-2-child-id> todo   # promote when its deps are met
```

`issue children --output json` reports per-stage `done` counts, including custom
statuses in terminal categories. When reading issue JSON, `status` is the exact
key; `status_category` retains the seven-value API enum for installed clients:
`backlog` / `todo` mean unstarted, `in_progress` / `in_review` / `blocked` mean
started, `done` means successful terminal, and `cancelled` means cancelled
terminal (the internal closed category). These values encode lifecycle, not
built-in automation behavior. Check `status_category` for `done` / `cancelled`
(or use the stage counts), not just the concrete `status` key, to recognize
terminal children.

Read each sub-issue's description before promoting and only promote items whose
stated dependencies are met; if a description conflicts with the parent's
breakdown, leave it `backlog` and comment to confirm first.

## Triage verdicts

Inbound work waits in the triage queue; agents suggest a verdict, humans
decide. Commands and rules: `references/triage-verdicts.md`.

## Incorrect to correct

PR title (link the issue):

```text
Fix login redirect                  # incorrect — no issue key, won't link
Body-only "Part of MUL-123"         # incorrect — passing mention, won't link
MUL-123: fix login redirect        # correct — links the PR
```

Serial / phased sub-issues: creating them all `todo` fires the whole chain at
once. Stage them instead — see "Stages: order sub-issues into barrier groups".

## Further reading

`references/undo-and-show-me-first.md` — the undo journal and the `202` "show
me first" contract: queued for approval, not done, not an error.
`references/goals.md` — proposing a workspace goal from a run.
`references/wakeups.md` — making an issue repeat, or waking its agent once.
