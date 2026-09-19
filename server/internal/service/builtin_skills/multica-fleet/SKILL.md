---
name: multica-fleet
description: "Answer questions about the workspace's agent fleet — who is running right now, what an agent has cost, daily activity history — via the multica fleet CLI. Use when asked about running agents, per-agent spend, or fleet activity over a period."
user-invocable: false
allowed-tools: Bash(multica *)
---

# Multica Fleet

The fleet commands answer "what are my agents doing and what did it cost" with
one call each, instead of scraping the dashboard surfaces. All three are
read-only member commands with stable JSON output.

```bash
multica fleet status  --output json
multica fleet cost    --output json
multica fleet history --output json
```

Shared flags:

- `--since <when>` — start of the window, RFC3339 or a bare `YYYY-MM-DD` date.
  Omit it for the default 30-day window. Say the window you used in your
  answer ("over the last 7 days"), because the default is easy to forget.
- `--agent-id <uuid>` — one agent only. Take the UUID from the JSON output of
  a fleet or agent list command; never type a display name where the id
  belongs, and never invent one.

`--output json` writes to stdout; warnings go to stderr. Do not merge them
(`2>&1`) into anything that parses the output.

## status — who is running

One row per agent: `agent_id`, `name`, `running_task_count` (in flight right
now), `task_count` and `failed_count` (terminal tasks completed inside the
window; failed is a subset of task_count, never added to it).

This is the only fleet response that carries agent names. When a cost or
history question needs names, join on `agent_id` against a status row or
`multica agent list --output json`.

## cost — what an agent spent

One row per agent: `agent_id`, `cost_usd_ticks`, `input_tokens`,
`output_tokens`, `task_count` over the window.

**Costs come back in pricing ticks, not dollars: 1 USD = 10_000_000_000 ticks
(1e10).** Always convert before reporting — divide by 1e10 and present USD
("$4.20"), never the raw tick figure. A tick count read as dollars is off by
ten orders of magnitude.

## history — activity over time

One row per (UTC calendar day, agent): `date` (`YYYY-MM-DD`), `agent_id`,
`task_count`, `failed_count`. Days with no completions produce no row — an
absent day means zero, not missing data.

## Reading the numbers

- Agents the caller may not see are folded into a single bucket whose
  `agent_id` is the `__restricted_agents__` sentinel and whose name is empty.
  The bucket's totals are real — include them in workspace-wide sums, and say
  "plus N tasks from agents you cannot see" rather than dropping them.
  Filtering by a hidden agent's id returns an empty list, not that agent's
  numbers — an empty result with `--agent-id` can mean "no activity" or "not
  visible to you"; do not assert which.
- An empty list with no filter means exactly that: nothing ran in the window.

## When the answer needs more than the fleet

These commands aggregate existing runs; they do not list individual runs,
issues, or live output. "What is agent X working on" is `running_task_count`
here and the run list elsewhere; "why did a run fail" is that run's transcript,
not a fleet number.
