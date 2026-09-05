# Goals with ancestry (K74)

A workspace has goals in a tree: the root goal is the mission, sub-goals hang
under it. Projects link to goals (n-n) and an issue names one goal or, without
one, inherits every goal linked to its project. Progress rolls up done issues
through the tree.

## What the brief carries

`## Mission and goals`, rendered between Goal Ancestry and Issue Metadata:

```text
- Be profitable (active)
  Success measure: positive cash flow by Q4
- Ship billing (active, due 2026-12-31)
  Bill every seat.
  Success measure: every seat invoiced
- Project: Billing
```

Mission first, the goal the issue serves last, eight levels at most, one KiB
of description per goal. Absent when the issue serves no goal. The chain is
resolved from `issue.goal_id`, else from the first goal linked to the project.

## What you may do

- Read: `GET /api/goals` (tree with progress), `GET /api/goals/{id}` (the goal
  and the issues that count for it, inherited ones included).
- Propose: `POST /api/issues/{id}/goal-proposal` `{"goal_id": "...", "reason": "..."}`
  (task token). Files a decision with two options: attach, or keep as is. A
  human decides; the attach option sets `issue.goal_id`. 409 when the issue
  already serves that goal or the same proposal is still pending; 400 without
  a reason.

## What you may not do

- Create, edit or delete a goal (member endpoints; an active goal needs a
  human owner).
- Set `goal_id` on an issue: a task-token `PUT /api/issues/{id}` ignores the
  field.

## Source

- `server/internal/handler/goal.go` (`resolveClaimMissionChain`,
  `ProposeIssueGoal`, `applyGoalForDecision`).
- `server/internal/daemon/execenv/runtime_config_sections.go` (`writeMissionChain`).
