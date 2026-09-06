# Pull request walkthrough runs

If your brief asks for a walkthrough of a pull request diff, you are on a
**read-only** run. Change nothing, run nothing, open nothing. The whole
deliverable is one answer.

## What the walkthrough is for

A reviewer opening the pull request already has the file list. What they do not
have is the order to read it in: which two functions carry the behaviour
change, which forty files are a regenerated client, which hunk is a rename that
reads like a rewrite. That ordering is the product. A walkthrough that restates
the diff has produced nothing.

## Answer format

End with **exactly one** fenced block. Nothing else in your answer is read.

````
```pr_walkthrough
{"groups":[
  {"title":"Retry the fetch on a 429",
   "kind":"core",
   "rationale":"The behaviour change: a throttled response is retried instead of surfacing as a failure.",
   "files":[
     {"path":"api/client.go",
      "hunks":[
        {"old_start":12,"new_start":12,
         "lines":"@@ -12,7 +12,9 @@",
         "explanation":"Wraps the call so a 429 returns a retryable error instead of the raw status.",
         "moved_from":""}
      ]}
   ]}
]}
```
````

Fields:

- `title` — a short phrase naming the change, not the files.
- `kind` — one of `core`, `test`, `generated`, `noise`. Anything else is stored
  as `noise`, so a group you meant as important would be filed as skippable.
  - `core` — the behaviour change the reviewer must read.
  - `test` — tests and fixtures for it.
  - `generated` — lockfiles, generated clients, snapshots, checked-in build output.
  - `noise` — formatting, import order, pure moves and renames.
- `rationale` — one paragraph: why this group exists and what it does as a unit.
- `files[].path`, `hunks[].old_start`, `hunks[].new_start`, `hunks[].lines` —
  **copy these from the diff verbatim.** They anchor your explanation to the
  lines the reviewer is looking at; a re-typed or guessed line number points at
  the wrong code.
- `hunks[].explanation` — what this hunk does and why, in the change's terms.
- `hunks[].moved_from` — the old path when the hunk is a move or rename, else
  `""`. This is how a reviewer skips a rename that reads as a rewrite.

Groups are re-ordered server-side into `core` → `test` → `generated` → `noise`,
so send them in whatever order you found them.

## When the diff was truncated

A pull request over the size cap is given to you partially, and the brief says
so along with how many files were withheld. Describe what you were given.
Do not guess at the files you cannot see, and do not report the truncation as
a failure — a partial walkthrough is the expected outcome there.
