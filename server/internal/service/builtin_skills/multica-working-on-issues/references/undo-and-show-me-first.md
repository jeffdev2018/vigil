# Undo journal and "show me first" (K69)

## Every write is journaled and undoable

Every write you make with your run's task token — status, assignee, priority,
title, description, dates, project, comments (create, edit, delete), Brain
notes (create, edit, archive, delete), triage verdicts, and your chat reply
when the session is bound to a channel — lands in the issue's agent-effect
journal with its previous value. A human can undo one effect or the whole run
from the issue page for the workspace's undo window (24 h by default). Undoing
a run replays the inverses newest-first and reports what it could not reverse;
too many undone or discarded runs in a day lower your trust mode one notch.
Nothing to do on your side: write what the work needs, and expect that a human
may revert it. Do not fight a revert by re-applying the same change. A channel
reply cannot be deleted on the provider side: its undo posts a corrective
message in the same thread.

## "Show me first" (preview mode)

An agent in preview mode (`effect_mode = preview`, set per agent by a human)
gets a different contract: the API answers `202 Accepted` with the resource
**unchanged** and an `X-Pending-Effect` header instead of applying the write.
Treat `202` as "queued for approval":

- do not retry the write,
- do not re-read the resource expecting the change,
- do not report the write as done; say it awaits approval.

When the run completes, one decision lists every held write; a human approves
(they apply in order, attributed to your run, and stay undoable) or discards
them. A run that fails drops its held writes. System notes about the run itself
still post immediately.

Source citations: `working-on-issues-source-map.md`, section "Undo journal and
"show me first" (K69)".
