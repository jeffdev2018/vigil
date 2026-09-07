# Spending real money

A run that is about to pay for something — a paid API, a third-party service,
anything that leaves the workspace as a charge — asks the platform first. The
workspace sets a threshold; under it the answer is immediate, over it a human
decides. This is the only path: there is no "just spend it and report".

- [The two questions](#the-two-questions)
- [Asking for a token](#asking-for-a-token)
- [When a human has to decide](#when-a-human-has-to-decide)
- [Spending the token](#spending-the-token)
- [Reading your own budget](#reading-your-own-budget)
- [What this is not](#what-this-is-not)

## The two questions

| Question | Command |
|---|---|
| May I spend $X on this? | `multica issue spend-token request "$MULTICA_TASK_ID"` |
| I am spending it now | `multica issue spend-token verify "$MULTICA_TASK_ID"` |

Both are run-scoped and answered with the token this run already carries. A run
can only ask about itself.

`--amount` is plain USD — `2`, `2.50`, `0.0001`. The API counts in
`amount_usd_ticks` (1 USD = 10,000,000,000 ticks) and the CLI converts exactly;
never hand-convert, and never read a tick figure as dollars.

## Asking for a token

```bash
multica issue spend-token request "$MULTICA_TASK_ID" \
  --amount 2.50 --purpose "OpenAI batch embeddings for the 4k-doc corpus"
```

Under the workspace threshold this returns a token straight away:

```json
{"token": "mst_…", "expires_at": "…", "amount_usd_ticks": 25000000000}
```

The token is valid for **5 minutes** and for **one** spend. Ask when the money
is about to move, not at the top of the run.

`--purpose` is required by the CLI even though the API accepts an empty one: it
is the entire body of the Decision Card a human is asked to approve. Name the
service and what it buys. "API call" is not a purpose anyone can act on.

## When a human has to decide

Over the threshold the server opens a **spend gate** instead and answers with
it. That is a normal outcome, not a failure — tell the two apart by the payload:
a token has `token`, a gate has `id` and `"status": "pending"`.

```json
{"id": "…", "gate_type": "spend", "status": "pending", "summary": "spend $500.00 · …"}
```

Nothing is spent until a human approves it. Two ways to continue:

```bash
# Hold until it is decided (up to 5 minutes), then collect the token:
multica issue spend-token request "$MULTICA_TASK_ID" --amount 500 --purpose "…" --wait

# Or come back later with the gate id:
multica issue spend-token request "$MULTICA_TASK_ID" --amount 500 --gate <gate-id>
```

`--wait` returns whichever it reaches first: the issued token, the resolved gate
(`denied` / `expired`), or the still-`pending` gate when the five minutes run
out. All three exit 0 — read `status`, do not read the exit code. A `denied` or
`expired` gate is final for that gate: do not re-ask for the same spend, say
what was refused and stop. Without an approved gate there is no token, and
without a token the charge cannot be verified, so the refusal holds whatever the
run does next.

Do not open a second gate for a spend a human already denied, and do not split
one charge into several under-threshold requests. Both defeat the threshold the
workspace configured, and both are visible in the gate history.

## Spending the token

```bash
multica issue spend-token verify "$MULTICA_TASK_ID" --token mst_… --amount 2.50
```

Call this when the charge is actually being made, so the record of what was
approved matches what was spent. `--amount` may not exceed the amount the token
was issued for. Refusals are explicit and final for that token:

| `code` | Meaning |
|---|---|
| `spend_token_used` | already spent — a token covers one charge |
| `spend_token_expired` | past its 5 minutes — ask again |
| `spend_over_cap` | costs more than was approved — ask for the real amount |
| `spend_token_invalid` | not a token of this run |

## Reading your own budget

```bash
multica issue budget-status "$MULTICA_TASK_ID"
```

Read-only. Returns `usage` (`cost_usd_ticks`, `duration_seconds`, `turns`,
`tool_calls`), the `gates` those are measured against — each with its `limit`,
`observed`, `ratio` and `level` (`warn`, `exceeded`, `stopped`) — and the
`events` already recorded. Use it before an expensive step to see how much room
is left, and after a warning to see which limit produced it.

Run limits and the spend gate are separate mechanisms: limits are caps the
platform enforces on the run itself, the spend gate is permission for money
leaving the workspace. Being under your run limits is not permission to spend.

## What this is not

A spend token is not a payment and not a credential. It records that a spend of
a stated size, for a stated purpose, was permitted for this run. Provider keys
and payment details come from the environment as they always did, and none of
them belong in `--purpose`, which a human reads.
