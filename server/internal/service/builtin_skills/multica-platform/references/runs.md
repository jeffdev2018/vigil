# The fleet: every run of the workspace

`multica runs list` shows the runs of the workspace — yours included —
newest first: agent, issue, status, **what each is blocked on** (an approval
gate, a Decision Card, a goal question, a held status change, a local
directory another run holds, a pause), when it started, what it cost.
The cost is the provider's reported cost, else a catalog estimate from the
run's tokens — the same pricing budgets settle on. In `--output json`,
`cost_known: false` means no usage could be priced: the cost is unknown, not
zero, and the summary's `cost_unknown_since` counts such runs.

- `multica runs list` — runs in flight. `--terminal` for finished ones,
  `--all` for both, `--agent <id>` / `--issue <id>` to narrow, `--cursor`
  from the stderr hint to page, `--output json` for the full rows.
- `multica runs cancel <run-id> [run-id...]` — one line per id: `cancelled`,
  `already_over`, `not_found`. A run that is already over is not an error.

## When to look

- Before starting work that touches what another run may be touching: a run
  on a sibling issue with the same paths, or a run blocked on a gate you are
  about to open again. `multica issue runs <issue-id> --siblings` answers the
  narrower question for one issue family.
- When your own run is waiting and you want to say so precisely in your
  status: the `blocked_on` of your run names the ask and where it is settled.

## What you do not do

- The kill switch (halt the fleet and cancel everything) is a person's
  decision in the app; it has no command. When the fleet is halted the list
  says so on stderr, and every gated action you attempt is refused with the
  halt's reason: stop and report, do not retry.
- Do not cancel other agents' runs to make room for yours. Cancel your own
  duplicate or stale runs, and say which ones in your closing status.
