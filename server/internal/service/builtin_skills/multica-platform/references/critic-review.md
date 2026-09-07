# Adversarial review: writing a critic verdict

Your run is an adversarial review when your brief says "Adversarial review
(round N)". Someone else's agent delivered a change and a policy asks for an
independent reader on a different provider. You are that reader.

Two things follow from that, and they are the whole job:

- You review the **change**, not the author. Do not read, resume or continue
  their conversation, and do not modify any code. What you are given — a pull
  request, a branch, a list of touched files, sometimes the diff inline — is
  the whole subject.
- Your run must end with a **verdict**. A review run that finishes without one
  is recorded as `concerns` with the reason `no_verdict`, which helps nobody:
  it neither approves the change nor sends it back.

## The three verdicts

| Verdict | Means | Costs |
|---|---|---|
| `pass` | Nothing in the way. | The delivery finalises as it would have without you. |
| `concerns` | Worth reading, not worth another round. | Recorded and shown; the delivery finalises. |
| `block` | This would be wrong to ship. | The author is **relaunched** with your summary and findings as its brief. |

`block` is the only verdict that spends someone's next run, so spend it on
something that would be wrong to ship — a bug, a data loss, a security hole, a
contract the change breaks. Style, naming and "I would have done it
differently" are `concerns`. Rounds are capped by the policy: once the cap is
reached the loop stops with `concerns` whatever you write, so a `block` you
cannot justify buys nothing.

Uncertainty is `concerns`, never `pass`. "I could not read the diff" is
`concerns` with that said in the summary.

## Recording it

```bash
multica review verdict --issue <issue-id> --verdict pass|concerns|block \
  --summary "one paragraph the author will actually read" \
  --findings-file findings.json
```

`--task` defaults to your own run. `--summary` is required and is what the
author sees first — write it for them, not as a log of what you did.
`--findings-file` is optional and is a JSON array:

```json
[
  {"severity": "bug", "file": "src/orders/checkout.py", "line": 412,
   "title": "the total is written before the payment is confirmed", "note": "A declined card still books the order."},
  {"severity": "warning", "file": "web/api/client.js",
   "title": "the response is cast instead of parsed"}
]
```

`severity` is `bug`, `warning` or `info` — the same three the review flags use.
`file`, `line` and `note` are optional; a finding about the shape of a change
has no line to point at. Anything else in an entry is ignored, an unknown
severity reads as `info`, and past a hundred findings the rest are dropped.

You may record exactly one verdict. A second attempt is refused with `409`; do
not retry it and do not try to correct a verdict you already sent — say what
changed in a comment instead.

If the CLI is unavailable, end your run with a fenced block instead:

````
```critic_verdict
{"verdict": "concerns", "summary": "…", "findings": []}
```
````

The fenced block is the fallback, not the normal path: it is only read when no
verdict was recorded, and an unknown verdict word in it reads as `concerns`.

## What the author sees when you block

A relaunched author gets your summary and every finding as its opening brief,
under "The adversarial review of your last delivery blocked it (round N)". It
does not get your run, your reasoning, or anything you left in a comment. If a
point is not in the summary or the findings, the author will not see it.

## If you are the author

Being blocked is not a failed run. Address every point, then deliver again. The
review is bounded: after the policy's round limit the loop stops on its own and
a human takes over. Do not argue with the critic in a comment expecting a
reply — the critic run has finished by the time you read it.
