# Insights

Ask the workspace a question in plain language, get a figure, pin it as a card.

The contract you need to know: **the model never writes SQL.** It emits a JSON
document conforming to a closed vocabulary, and the server compiles that
document. Nothing you write is executed as text.

## What the vocabulary accepts

```json
{
  "entity":      "issue" | "task" | "comment",
  "metric":      "count" | "sum:<numeric field>" | "avg_age_days" | "p50_cycle_time",
  "group_by":    ["status", "time", "property:<uuid>", ...],   // at most 2
  "filters":     [{"field": "status", "op": "is", "values": ["blocked"]}],
  "time_range":  {"field": "created_at", "last_days": 30},
  "granularity": "day" | "week" | "month",
  "limit":       1..200
}
```

Per entity:

| Entity | group_by | filter fields | extra metrics |
|---|---|---|---|
| `issue` | `status`, `priority`, `assignee`, `project`, `label`, `issue_type`, `time`, `property:<uuid>` | the same, plus `age_days` and `idle_days` | `sum:reopen_count`, `sum:stage` |
| `task` | `status`, `agent`, `time` | `status`, `agent`, `age_days`, `idle_days` | `sum:attempt` |
| `comment` | `author`, `author_type`, `type`, `time` | the same, plus `age_days` and `idle_days` | — |

Operators: `is`, `is_not`, `in`, `not_in` take values; `gt`, `gte`, `lt`,
`lte` take exactly one number and apply **only** to `age_days` and
`idle_days`; `is_empty` and `is_not_empty` take none.

Anything outside this list is a 400 before anything runs. There is no free
text, no join vocabulary, and no way to name a table.

## Statuses are workspace-defined

Never assume `done` or `blocked` exists. A workspace can rename and add
statuses, so read the catalogue (`multica issue status list --output json`, or
`GET /api/issue-statuses`) and use the keys it returns. A key that does not
exist compiles fine and counts zero, which is the failure mode that looks like
an answer.

## Endpoints

| Route | What it does |
|---|---|
| `POST /api/insights/ask` `{question}` | Translates, executes, returns `{query, rows, shape, warnings}`. Needs the assist layer; 503 without one. |
| `POST /api/insights/run` `{query}` | Executes a document. **No model.** This is what a pinned card calls on every refresh. |
| `GET/POST /api/insights/widgets` | List / pin. `visibility: workspace` is owner/admin only; `private` is the default. |
| `PATCH/DELETE /api/insights/widgets/{id}` | PATCH requires `expected_revision` and answers 409 when it does not match. |

A pinned widget stores the document, not the question's answer and not a
translation. Refreshing it re-runs the stored document, so a pinned figure
cannot change meaning because a later translation drifted.

A document carrying a field the vocabulary does not define is rejected, not
ignored — including a forged `workspace_id`. The workspace always comes from
the authenticated request.

## What the numbers do not mean

The schema keeps no status-transition history, so v1 cannot measure time spent
in a status.

- `age_days` counts from creation.
- `idle_days` counts since the row last moved. It is the closest available
  stand-in for "has been sitting in this state for N days" — say so rather than
  reporting it as time-in-status.
- `p50_cycle_time` is creation to completion.
- Grouping issues by `label` counts a multi-labelled issue once per label.

Every response carries these caveats in `warnings`; repeat them when you quote
a figure.

## Reporting a figure

Always quote the question the figure answers. A number whose question is
invisible cannot be checked, and a plausible wrong number is worse than no
number.
