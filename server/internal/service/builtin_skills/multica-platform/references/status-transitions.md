# Status transitions and approval gates

A workspace can declare who may move an issue from one status category to
another, and which of those moves wait for a human approver. Rules are written
against CATEGORIES (backlog, todo, in_progress, in_review, done, blocked,
cancelled), not against status names, and a project rule overrides the
workspace rule for issues in that project. Where no rule matches, the move is
free — most workspaces have no rules at all and nothing changes for you.

You cannot read a rule into existence or edit one: rules are workspace
configuration, owned by owners and admins. What you can do is read what your
own next move would cost before you attempt it, and answer the two refusals
correctly when you get them.

## `403 transition_not_allowed`

No rule grants you this move.

```json
{"code": "transition_not_allowed", "from": "in_progress", "to": "done", "rule_id": "..."}
```

The issue is unchanged. Do NOT retry it, do not try a different route to the
same status, and do not work around it by editing something else. The refusal
is a decision a human wrote down; retrying it just fails again and buries the
real message.

What to do instead: leave a comment on the issue saying the work is finished
and which status you were unable to set, then stop. Your run ends as normal —
being refused a status change is not a failed run.

## `202` with `pending_approval`

The rule allows you this move, but it waits for an approver.

```json
{"status": "pending_approval", "request_id": "...", "issue": {"status": "in_progress"}}
```

The issue is **unchanged**; the echoed `issue` is the current state, not the
requested one. Treat `202` here exactly as you treat the "show me first" 202:

- do not retry the write,
- do not re-read the issue expecting the new status,
- do not report the status change as done — say it awaits approval, and name
  the request id.

An approver then applies it or rejects it. A rejection may put the issue on a
different status than the one you asked for: the rule can name a fallback, and
`blocked` is a common choice. Read the issue again at the start of your next
turn rather than assuming where it landed.

`409 transition_pending` means the issue already has a request waiting. Another
run or a person asked for a status change first. Nothing to do: do not file a
second one, and do not retry.

## Checking before you write

```bash
multica issue update <issue> --status done
```

The CLI reports both answers as an explicit failure with a non-zero exit code,
so a script that only checks the exit code will not mistake either for success.
There is no "force" flag and there is no way to ask for an exemption.

Two moves are never gated, so you can rely on them: creating an issue in the
default `todo` status, and any status change the platform makes on its own
behalf (a merged pull request closing its issue, the sweeper returning an
abandoned issue to `todo`).
