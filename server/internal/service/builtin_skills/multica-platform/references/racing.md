# Racing several attempts on one issue

When one issue has several plausible approaches, queue them at once and keep
one. Each attempt is an ordinary run on its own branch, so a race of four costs
four runs.

```bash
multica issue race start <issue-id> --attempt "Backend Dev" --attempt "Backend Dev:opus" --attempt "Refactor Bot" --note "keep the public API"
multica issue race list <issue-id> --output json
```

`--attempt` is `<agent>[:<model>]` and repeats 2 to 5 times; the same agent may
appear twice on two models. One open race per issue: start is refused (`409`)
until the current one is settled or abandoned.

Settling and abandoning are **human-only** — an agent calling them gets `403`.
A human picks the winner, and every other attempt still open is cancelled, so
the daemon drops its branch and worktree:

```bash
multica issue race settle <issue-id> --group <race-id> --winner <run-id>
multica issue race abandon <issue-id> --group <race-id>
```

`race list` reports each attempt's diff as `stat + patch`, or `stat only (too
large)` when the patch exceeded the stored bound — that is not "no changes",
it means read the branch itself.
