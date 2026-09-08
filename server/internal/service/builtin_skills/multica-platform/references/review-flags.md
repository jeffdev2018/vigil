# Review flags

A review flag is one structured finding: a file, a line range of one revision
of a pull request, a severity, and a title. It is how a run hands a reviewer
something they can sort, count and settle — as opposed to prose in a comment,
which they have to read in full to find out whether it contains anything.

Recording one is a write with a consequence: the flag stays on the issue until
a human resolves or dismisses it. Nothing you write here expires on its own.

## Recording one

```
multica review flag add \
  --issue MUL-123 \
  --pr <pull-request-uuid> \
  --file src/checkout/total.py \
  --line 42-48 \
  --severity bug \
  --confidence 80 \
  --title "the retry path swallows the fetch error" \
  --body "The 429 branch returns nil, so the caller reports success."
```

The pull request UUID comes from `multica issue pull-requests --output json` on
the same issue. A pull request that is not linked to that issue answers 404 —
the flag hangs off the issue, so an unlinked PR has nowhere to put it.

`--line` takes `42` for one line or `42-48` for a range. `--side old` points at
a deleted line; the default `new` is the changed code. `--sha` is optional and
defaults to the pull request's current head, which is what you want unless you
are deliberately flagging an older revision.

`--body -` reads the long form from stdin, so a multi-paragraph finding does
not have to survive shell quoting.

## Severity is what the reviewer sorts by

- `bug` — this is wrong. It produces an incorrect result, loses data, or breaks
  a contract the rest of the code relies on.
- `warning` — this is risky. It works today and will bite under a condition you
  can name: a size, a race, a missing index, an unhandled provider error.
- `info` — worth knowing, nothing owed. A convention, a simpler alternative, a
  place the next change will have to touch.

Severity outranks confidence in the sort, so a `bug` you are 40% sure of is
read before a `warning` you are certain of. That is deliberate, and it is why
calling a style preference a `bug` costs the reviewer real time: it puts your
opinion above someone else's correctness finding.

An unknown severity is refused. Use one of the three.

## Confidence is optional

`--confidence` is 0-100 and only orders flags of the same severity. Omit it
when you would be guessing at the number — "did not say" sorts after every
stated confidence, which is honest, whereas an invented `50` claims a
calibration you do not have.

## One flag per finding

Two problems in the same function are two flags, at their own line ranges. A
flag whose body lists three unrelated issues cannot be resolved: the reviewer
fixes one of them and the flag is still open, saying nothing about which.

A single run may record at most 100 flags. Hitting that cap answers
`too_many_flags` and means the run has stopped reviewing and started linting —
the reviewer would never read past the first screen. Report the pattern once at
the severity it deserves rather than once per occurrence.

## What you cannot do

Resolving and dismissing are the reviewer's, not yours. `multica review flag
list --issue <id> --state all` shows every flag with its state so you can read
what was already decided; there is no command that settles one.

## When the code moves

A push that moves the pull request's head marks every open flag written against
the old head `stale`. Nothing is deleted — the finding was true of the code it
was written against, and the reviewer may still need that record. A stale flag
is not a failure and does not need re-filing unless the problem is still there
on the new head, in which case flag it again at the new line range.
